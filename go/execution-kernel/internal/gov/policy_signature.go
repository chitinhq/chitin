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

type PolicyLoadOptions struct {
	BypassSignature  bool
	PublicKey        string
	TrustDir         string
	RequireSignature bool
}

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

func VerifyPolicySignatureFile(path string, opts PolicyLoadOptions) error {
	if opts.BypassSignature {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read policy: %w", err)
	}
	return VerifyPolicySignatureBytes(path, data, opts)
}

func VerifyPolicySignatureBytes(path string, data []byte, opts PolicyLoadOptions) error {
	if opts.BypassSignature {
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

	requireSig := opts.signatureRequired() || trustConfigured
	if errors.Is(sigErr, os.ErrNotExist) {
		if requireSig {
			return &PolicySignatureError{Code: "policy_signature_missing", Path: path, Message: "missing sidecar signature " + filepath.Base(sigPath)}
		}
		return nil
	}
	if !trustConfigured {
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
