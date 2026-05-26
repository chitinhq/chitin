package blob

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const blobDirEnv = "CHITIN_BLOB_DIR"

// BlobDirEnv is the environment variable that overrides the filesystem store
// root.
const BlobDirEnv = blobDirEnv

// BlobWrittenPayload is the blob_written chain-event payload.
type BlobWrittenPayload struct {
	Ref       string `json:"ref"`
	SizeBytes int    `json:"size_bytes"`
	SHA256    string `json:"sha256"`
}

// Emitter records a successful blob write in the audit chain.
type Emitter interface {
	EmitBlobWritten(ctx context.Context, payload BlobWrittenPayload) error
}

// EmitterFunc adapts a function into an Emitter.
type EmitterFunc func(context.Context, BlobWrittenPayload) error

func (f EmitterFunc) EmitBlobWritten(ctx context.Context, payload BlobWrittenPayload) error {
	return f(ctx, payload)
}

// FSStore stores blobs under a local sharded directory.
type FSStore struct {
	dir     string
	emitter Emitter
}

// FSOption configures an FSStore.
type FSOption func(*FSStore)

// WithDir overrides the filesystem blob directory.
func WithDir(dir string) FSOption {
	return func(s *FSStore) {
		if dir != "" {
			s.dir = dir
		}
	}
}

// WithEmitter overrides chain-event emission. A nil emitter disables emission.
func WithEmitter(emitter Emitter) FSOption {
	return func(s *FSStore) {
		s.emitter = emitter
	}
}

// NewFSStore returns the default filesystem Store.
func NewFSStore(opts ...FSOption) *FSStore {
	s := &FSStore{
		dir:     DefaultDir(),
		emitter: KernelEmitter{},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// DefaultDir resolves the active filesystem blob directory.
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

// Dir returns the filesystem root for blobs.
func (s *FSStore) Dir() string {
	if s == nil || s.dir == "" {
		return DefaultDir()
	}
	return s.dir
}

// Put stores body by SHA-256 and returns its blob URI.
func (s *FSStore) Put(ctx context.Context, body []byte) (Ref, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	ref, err := NewRef(hash)
	if err != nil {
		return "", err
	}
	path := s.pathForHash(hash)
	if _, err := os.Stat(path); err == nil {
		return ref, nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+hash+".*.tmp")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Link(tmpPath, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return ref, nil
		}
		return "", err
	}
	cleanup = true
	if err := fsyncDir(filepath.Dir(path)); err != nil {
		return "", err
	}
	if s != nil && s.emitter != nil {
		payload := BlobWrittenPayload{Ref: ref.String(), SizeBytes: len(body), SHA256: hash}
		if err := s.emitter.EmitBlobWritten(ctx, payload); err != nil {
			return "", err
		}
	}
	return ref, nil
}

// Get reads a blob body by reference.
func (s *FSStore) Get(ctx context.Context, ref Ref) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	hash, err := ref.SHA256()
	if err != nil {
		return nil, err
	}
	if hash == "" {
		return nil, fmt.Errorf("blob: empty ref")
	}
	body, err := os.ReadFile(s.pathForHash(hash))
	if err != nil {
		return nil, err
	}
	return body, nil
}

func (s *FSStore) pathForHash(hash string) string {
	return filepath.Join(s.Dir(), hash[:2], hash[2:]+".blob")
}

func fsyncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// KernelEmitter emits blob_written through the kernel emit subcommand.
type KernelEmitter struct{}

func (KernelEmitter) EmitBlobWritten(ctx context.Context, payload BlobWrittenPayload) error {
	if os.Getenv("CHITIN_DISABLE_CHAIN_EMIT") == "1" {
		return nil
	}
	binPath := os.Getenv("CHITIN_KERNEL_BIN")
	if binPath == "" {
		binPath = "chitin-kernel"
	}
	envelope := map[string]any{
		"schema_version":    "2",
		"event_type":        "blob_written",
		"run_id":            payload.SHA256,
		"session_id":        "chitin-orchestrator-blob-" + payload.SHA256[:12],
		"surface":           "chitin-orchestrator",
		"agent_instance_id": "chitin-orchestrator",
		"chain_type":        "blob-store",
		"ts":                time.Now().UTC().Format(time.RFC3339Nano),
		"payload":           payload,
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "chitin-blob-emit-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
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
		if tail != "" {
			return fmt.Errorf("blob: chain emit failed: %w: %s", err, tail)
		}
		return fmt.Errorf("blob: chain emit failed: %w", err)
	}
	return nil
}

var _ Store = (*FSStore)(nil)
var _ Emitter = EmitterFunc(nil)
