package blob

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const blobDirEnv = "CHITIN_BLOB_DIR"

// BlobWrittenPayload is emitted after a new blob lands on disk.
type BlobWrittenPayload struct {
	Ref       string `json:"ref"`
	SizeBytes int    `json:"size_bytes"`
	SHA256    string `json:"sha256"`
}

// EventSink records blob store audit events.
type EventSink interface {
	BlobWritten(ctx context.Context, payload BlobWrittenPayload)
}

// EventSinkFunc adapts a function to EventSink.
type EventSinkFunc func(ctx context.Context, payload BlobWrittenPayload)

// BlobWritten implements EventSink.
func (f EventSinkFunc) BlobWritten(ctx context.Context, payload BlobWrittenPayload) {
	if f != nil {
		f(ctx, payload)
	}
}

// FSStore stores blobs under a sharded filesystem directory.
type FSStore struct {
	dir              string
	events           EventSink
	afterTempWrite   func() error
	beforeRename     func() error
	disableDirFsync  bool
	disableFileFsync bool
}

// FSOption configures an FSStore.
type FSOption func(*FSStore)

// WithEventSink sets the audit event sink. A nil sink disables emission.
func WithEventSink(sink EventSink) FSOption {
	return func(s *FSStore) {
		s.events = sink
	}
}

// WithKernelEventSink emits blob_written events through chitin-kernel.
func WithKernelEventSink(stderr io.Writer) FSOption {
	return WithEventSink(KernelEventSink{Stderr: stderr})
}

// NewFSStore returns an FSStore rooted at dir. Empty dir resolves from env.
func NewFSStore(dir string, opts ...FSOption) (*FSStore, error) {
	if dir == "" {
		dir = DefaultDir()
	}
	if dir == "" {
		return nil, fmt.Errorf("blob: empty filesystem store directory")
	}
	s := &FSStore{dir: dir}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// NewFSStoreFromEnv returns the production default filesystem store.
func NewFSStoreFromEnv(opts ...FSOption) (*FSStore, error) {
	all := []FSOption{WithKernelEventSink(os.Stderr)}
	all = append(all, opts...)
	return NewFSStore("", all...)
}

// Dir returns the root directory where blobs are stored.
func (s *FSStore) Dir() string { return s.dir }

// DefaultDir returns $CHITIN_BLOB_DIR or ~/.chitin/blobs.
func DefaultDir() string {
	if dir := strings.TrimSpace(os.Getenv(blobDirEnv)); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".chitin", "blobs")
	}
	return filepath.Join(home, ".chitin", "blobs")
}

// Put writes body to its content-addressed path and returns its blob ref.
func (s *FSStore) Put(ctx context.Context, body []byte) (Ref, error) {
	if s == nil {
		return Ref{}, fmt.Errorf("blob: nil FSStore")
	}
	if err := ctx.Err(); err != nil {
		return Ref{}, err
	}
	hash := hashBody(body)
	ref, err := RefFromHash(hash)
	if err != nil {
		return Ref{}, err
	}
	dest := s.pathForHash(hash)
	if _, err := os.Stat(dest); err == nil {
		return ref, nil
	} else if !os.IsNotExist(err) {
		return Ref{}, err
	}

	shardDir := filepath.Dir(dest)
	if err := os.MkdirAll(shardDir, 0o755); err != nil {
		return Ref{}, err
	}
	tmp, err := os.CreateTemp(shardDir, "."+hash+".*.tmp")
	if err != nil {
		return Ref{}, err
	}
	tmpPath := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return Ref{}, err
	}
	if !s.disableFileFsync {
		if err := tmp.Sync(); err != nil {
			_ = tmp.Close()
			return Ref{}, err
		}
	}
	if err := tmp.Close(); err != nil {
		return Ref{}, err
	}
	if s.afterTempWrite != nil {
		return Ref{}, s.afterTempWrite()
	}
	if s.beforeRename != nil {
		if err := s.beforeRename(); err != nil {
			return Ref{}, err
		}
	}
	// Atomic publish via hard link: link(2) fails with EEXIST if dest already
	// exists, so concurrent identical Puts cannot rewrite the destination or
	// double-emit blob_written. Unlike os.Rename (which silently replaces on
	// Unix), this enforces the "one writer ever lands a given hash" invariant.
	// The temp inode is cleaned up by the defer; dest holds its own link.
	if err := os.Link(tmpPath, dest); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return ref, nil
		}
		return Ref{}, err
	}
	if !s.disableDirFsync {
		if err := fsyncDir(shardDir); err != nil {
			return Ref{}, err
		}
	}
	if s.events != nil {
		s.events.BlobWritten(ctx, BlobWrittenPayload{Ref: ref.String(), SizeBytes: len(body), SHA256: hash})
	}
	return ref, nil
}

// Get reads the bytes for ref.
func (s *FSStore) Get(ctx context.Context, ref Ref) ([]byte, error) {
	if s == nil {
		return nil, fmt.Errorf("blob: nil FSStore")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	hash, err := ref.Hash()
	if err != nil {
		return nil, err
	}
	body, err := os.ReadFile(s.pathForHash(hash))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("blob: %s not found: %w", ref.String(), err)
		}
		return nil, err
	}
	if got := hashBody(body); got != hash {
		return nil, fmt.Errorf("blob: %s hash mismatch: got %s", ref.String(), got)
	}
	return body, nil
}

func (s *FSStore) pathForHash(hash string) string {
	return filepath.Join(s.dir, hash[:2], hash[2:]+".blob")
}

func hashBody(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func fsyncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// KernelEventSink emits chain events through chitin-kernel.
type KernelEventSink struct {
	Stderr io.Writer
}

// BlobWritten emits one blob_written event. Failures are warning-only.
// Honors CHITIN_DISABLE_CHAIN_EMIT=1 like the deliver/pr-iteration/sibling-rebase
// emitters so sandboxed tests and dry-run sessions can suppress chain side effects.
func (s KernelEventSink) BlobWritten(ctx context.Context, payload BlobWrittenPayload) {
	if os.Getenv("CHITIN_DISABLE_CHAIN_EMIT") == "1" {
		return
	}
	binPath := os.Getenv("CHITIN_KERNEL_BIN")
	if binPath == "" {
		binPath = "chitin-kernel"
	}
	agentID := fmt.Sprintf("chitin-orchestrator-blob-%d", os.Getpid())
	envelope := map[string]any{
		"schema_version":    "2",
		"event_type":        "blob_written",
		"run_id":            payload.SHA256,
		"session_id":        agentID,
		"surface":           "chitin-orchestrator",
		"agent_instance_id": agentID,
		"chain_type":        "operator-cli",
		"ts":                time.Now().UTC().Format(time.RFC3339Nano),
		"payload":           payload,
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		s.warn("chain emit failed: marshal blob_written: %v", err)
		return
	}
	tmpFile, err := os.CreateTemp("", "chitin-blob-emit-*.json")
	if err != nil {
		s.warn("chain emit failed: create temp file: %v", err)
		return
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	if _, err := tmpFile.Write(body); err != nil {
		_ = tmpFile.Close()
		s.warn("chain emit failed: write temp file: %v", err)
		return
	}
	if err := tmpFile.Close(); err != nil {
		s.warn("chain emit failed: close temp file: %v", err)
		return
	}
	chitinDir := os.Getenv("CHITIN_DIR")
	if chitinDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			chitinDir = filepath.Join(home, ".chitin")
		} else {
			chitinDir = ".chitin"
		}
	}
	emitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(emitCtx, binPath, "emit", "-dir", chitinDir, "-event-file", tmpPath)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		tail := strings.TrimSpace(stderr.String())
		if len(tail) > 200 {
			tail = tail[len(tail)-200:]
		}
		s.warn("chain emit failed: %v (stderr: %s)", err, tail)
	}
}

func (s KernelEventSink) warn(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if s.Stderr != nil {
		fmt.Fprintln(s.Stderr, "warning: "+msg)
		return
	}
	log.Printf("warning: %s", msg)
}

func equalBytes(a, b []byte) bool {
	return bytes.Equal(a, b)
}
