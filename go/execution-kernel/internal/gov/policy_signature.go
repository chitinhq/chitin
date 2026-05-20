// Package gov includes the kernel's Ed25519 signature support for
// chitin.yaml. Signing is operator-driven via the
// `chitin-kernel policy {keygen,sign,verify}` subcommands and the
// pre-commit hook installed by
// `scripts/install-governance-policy-signing-hook.sh`.
//
// At policy load time the kernel calls VerifyPolicySignatureFile (or
// VerifyPolicySignatureBytes) before parsing the YAML, refusing to
// continue if a pinned operator public key fails verification.
//
// Operator-presence bypass: when CHITIN_GOV_OPERATOR_AUTHORIZED=1 is
// set in the calling process env, signature verification is skipped.
// Same env-var contract the rule-eval bypass in gate.go honors (see
// Gate.Evaluate § 4.6) — the operator is sitting in the loop and their
// presence is the trust signal, so each worktree no longer needs its
// own copy of the sidecar to be usable interactively. Autonomous
// workers (clawta-poller, kanban-dispatch.lobster, mini watch) MUST
// scrub this env var before spawn so they fall back to sig-required
// mode; see swarm/workflows/spawn_worker_subprocess.py and its
// regression test.

package gov

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	PolicySignatureAlgorithm = "ed25519"
	DefaultPolicyPublicKey   = "chitin-policy-ed25519.pub"
	DefaultPolicySigSuffix   = ".sig"
)

// PolicyLoadOptions selects the trust roots and required-signature
// stance applied when the kernel verifies a chitin.yaml signature.
// Zero value defaults to "advisory" (verify when both a trust key
// and a sidecar signature are present) and honors the
// CHITIN_POLICY_REQUIRE_SIGNATURE and CHITIN_POLICY_TRUST_DIR env
// vars when their corresponding struct fields are empty.
type PolicyLoadOptions struct {
	BypassSignature  bool
	PublicKey        string
	TrustDir         string
	RequireSignature bool
}

// PolicySignatureError is returned by VerifyPolicySignatureFile /
// VerifyPolicySignatureBytes when verification cannot succeed.
// Code is a stable identifier (e.g. policy_signature_missing) suitable
// for routing and audit logging; Path is the file involved when
// known; Message is a free-form detail string.
type PolicySignatureError struct {
	Code    string
	Path    string
	Message string
}

func (e *PolicySignatureError) Error() string {
	if e.Path == "" {
		return e.Code + ": " + e.Message
	}
	return e.Code + ": " + e.Path + ": " + e.Message
}

// IsPolicySignatureError reports whether err (or anything it wraps) is
// a *PolicySignatureError. Used by callers that want to surface a
// signature-specific exit code without inspecting the wrapped chain.
func IsPolicySignatureError(err error) bool {
	var sigErr *PolicySignatureError
	return errors.As(err, &sigErr)
}

func (o PolicyLoadOptions) signatureRequired() bool {
	if o.RequireSignature {
		return true
	}
	v := strings.TrimSpace(os.Getenv("CHITIN_POLICY_REQUIRE_SIGNATURE"))
	return v == "1" || strings.EqualFold(v, "true")
}

func (o PolicyLoadOptions) trustDir() string {
	if o.TrustDir != "" {
		return o.TrustDir
	}
	if v := strings.TrimSpace(os.Getenv("CHITIN_POLICY_TRUST_DIR")); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".chitin", "trust")
}

// operatorPresenceBypass reports whether CHITIN_GOV_OPERATOR_AUTHORIZED=1
// is set in the env. When true, signature verification short-circuits
// to success — operator presence is the trust signal. Autonomous
// worker spawns MUST scrub this env var; see package doc comment.
func operatorPresenceBypass() bool {
	return os.Getenv("CHITIN_GOV_OPERATOR_AUTHORIZED") == "1"
}

// VerifyPolicySignatureFile reads the policy file at path and
// verifies the sidecar `<path>.sig` against the resolved trust key.
// It returns nil when verification succeeds, when no signature is
// required, when opts.BypassSignature is true, or when the operator
// presence env var is set; otherwise it returns a
// *PolicySignatureError describing the failure.
func VerifyPolicySignatureFile(path string, opts PolicyLoadOptions) error {
	if opts.BypassSignature || operatorPresenceBypass() {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read policy: %w", err)
	}
	return VerifyPolicySignatureBytes(path, data, opts)
}

// VerifyPolicySignatureBytes is the bytes-already-in-memory variant of
// VerifyPolicySignatureFile. The sidecar `<path>.sig` is still read
// from disk so callers can verify a policy that came from a non-file
// source (e.g. inheritance) without re-reading it.
func VerifyPolicySignatureBytes(path string, data []byte, opts PolicyLoadOptions) error {
	if opts.BypassSignature || operatorPresenceBypass() {
		return nil
	}

	pub, trustConfigured, err := resolvePolicyPublicKey(opts)
	if err != nil {
		return err
	}
	sigPath := path + DefaultPolicySigSuffix
	sigData, sigErr := os.ReadFile(sigPath)
	if sigErr != nil && !errors.Is(sigErr, os.ErrNotExist) {
		return &PolicySignatureError{Code: "policy_signature_unreadable", Path: sigPath, Message: sigErr.Error()}
	}

	signatureRequired := opts.signatureRequired()
	requireSig := signatureRequired || trustConfigured
	if errors.Is(sigErr, os.ErrNotExist) {
		if requireSig {
			return &PolicySignatureError{Code: "policy_signature_missing", Path: path, Message: "missing sidecar signature " + filepath.Base(sigPath)}
		}
		return nil
	}
	if !trustConfigured {
		if !signatureRequired {
			return nil
		}
		return &PolicySignatureError{Code: "policy_signature_untrusted", Path: path, Message: "signature exists but no operator public key is pinned"}
	}

	sig, err := parsePolicySignature(sigData)
	if err != nil {
		return &PolicySignatureError{Code: "policy_signature_invalid", Path: sigPath, Message: err.Error()}
	}
	if !ed25519.Verify(pub, data, sig) {
		return &PolicySignatureError{Code: "policy_signature_invalid", Path: path, Message: "signature does not verify against pinned operator public key"}
	}
	return nil
}

func SignPolicyBytes(data []byte, privateKeyText string) (string, error) {
	priv, err := parsePolicyPrivateKey([]byte(privateKeyText))
	if err != nil {
		return "", err
	}
	sig := ed25519.Sign(priv, data)
	return base64.StdEncoding.EncodeToString(sig) + "\n", nil
}

func GeneratePolicyKeyPair() (publicKey, privateKey string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(pub) + "\n", base64.StdEncoding.EncodeToString(priv) + "\n", nil
}

func resolvePolicyPublicKey(opts PolicyLoadOptions) (ed25519.PublicKey, bool, error) {
	text := strings.TrimSpace(opts.PublicKey)
	if text == "" {
		text = strings.TrimSpace(os.Getenv("CHITIN_POLICY_PUBLIC_KEY"))
	}
	if text == "" {
		trustDir := opts.trustDir()
		if trustDir == "" {
			return nil, false, nil
		}
		path := filepath.Join(trustDir, DefaultPolicyPublicKey)
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		if err != nil {
			return nil, true, &PolicySignatureError{Code: "policy_trust_unreadable", Path: path, Message: err.Error()}
		}
		text = string(data)
	}
	pub, err := parsePolicyPublicKey([]byte(text))
	if err != nil {
		return nil, true, &PolicySignatureError{Code: "policy_trust_invalid", Message: err.Error()}
	}
	return pub, true, nil
}

func parsePolicyPublicKey(data []byte) (ed25519.PublicKey, error) {
	raw, err := decodeKeyText(data, "public_key")
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("public key length=%d want %d", len(raw), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(raw), nil
}

func parsePolicyPrivateKey(data []byte) (ed25519.PrivateKey, error) {
	raw, err := decodeKeyText(data, "private_key")
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("private key length=%d want %d", len(raw), ed25519.PrivateKeySize)
	}
	return ed25519.PrivateKey(raw), nil
}

func parsePolicySignature(data []byte) ([]byte, error) {
	raw, err := decodeKeyText(data, "signature")
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.SignatureSize {
		return nil, fmt.Errorf("signature length=%d want %d", len(raw), ed25519.SignatureSize)
	}
	return raw, nil
}

func decodeKeyText(data []byte, jsonField string) ([]byte, error) {
	text := strings.TrimSpace(string(data))
	if text == "" {
		return nil, fmt.Errorf("%s is empty", jsonField)
	}
	if strings.HasPrefix(text, "{") {
		var obj map[string]string
		if err := json.Unmarshal([]byte(text), &obj); err != nil {
			return nil, fmt.Errorf("parse json: %w", err)
		}
		if alg := obj["algorithm"]; alg != "" && alg != PolicySignatureAlgorithm {
			return nil, fmt.Errorf("algorithm=%q want %q", alg, PolicySignatureAlgorithm)
		}
		text = strings.TrimSpace(obj[jsonField])
		if text == "" {
			return nil, fmt.Errorf("json field %q is required", jsonField)
		}
	}
	text = strings.TrimPrefix(text, PolicySignatureAlgorithm+":")
	raw, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		return nil, fmt.Errorf("base64 decode: %w", err)
	}
	return raw, nil
}
