package gov

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const signedPolicyFixture = `
id: signed-policy-test
mode: enforce
rules:
  - id: allow-read
    action: file.read
    effect: allow
`

func writeSignedPolicy(t *testing.T, dir string) (policyPath string, opts PolicyLoadOptions) {
	t.Helper()
	pub, priv, err := GeneratePolicyKeyPair()
	if err != nil {
		t.Fatalf("GeneratePolicyKeyPair: %v", err)
	}
	policyPath = filepath.Join(dir, "chitin.yaml")
	if err := os.WriteFile(policyPath, []byte(signedPolicyFixture), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	sig, err := SignPolicyBytes([]byte(signedPolicyFixture), priv)
	if err != nil {
		t.Fatalf("SignPolicyBytes: %v", err)
	}
	if err := os.WriteFile(policyPath+DefaultPolicySigSuffix, []byte(sig), 0o644); err != nil {
		t.Fatalf("write sig: %v", err)
	}
	trustDir := filepath.Join(dir, ".chitin", "trust")
	if err := os.MkdirAll(trustDir, 0o755); err != nil {
		t.Fatalf("mkdir trust: %v", err)
	}
	if err := os.WriteFile(filepath.Join(trustDir, DefaultPolicyPublicKey), []byte(pub), 0o644); err != nil {
		t.Fatalf("write public key: %v", err)
	}
	return policyPath, PolicyLoadOptions{TrustDir: trustDir}
}

func TestLoadPolicyFileWithOptions_VerifiesSignedPolicy(t *testing.T) {
	policyPath, opts := writeSignedPolicy(t, t.TempDir())
	p, err := LoadPolicyFileWithOptions(policyPath, opts)
	if err != nil {
		t.Fatalf("LoadPolicyFileWithOptions: %v", err)
	}
	if p.ID != "signed-policy-test" {
		t.Fatalf("policy ID=%q", p.ID)
	}
}

func TestLoadPolicyFileWithOptions_RejectsTamperedSignedPolicy(t *testing.T) {
	dir := t.TempDir()
	policyPath, opts := writeSignedPolicy(t, dir)
	if err := os.WriteFile(policyPath, []byte(strings.Replace(signedPolicyFixture, "allow-read", "allow-read-tampered", 1)), 0o644); err != nil {
		t.Fatalf("tamper policy: %v", err)
	}
	_, err := LoadPolicyFileWithOptions(policyPath, opts)
	if err == nil {
		t.Fatal("expected signature verification error")
	}
	if !IsPolicySignatureError(err) {
		t.Fatalf("expected PolicySignatureError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "policy_signature_invalid") {
		t.Fatalf("error should be structured as policy_signature_invalid, got %v", err)
	}
}

func TestLoadPolicyFileWithOptions_BypassSignatureAllowsTamperedPolicy(t *testing.T) {
	dir := t.TempDir()
	policyPath, opts := writeSignedPolicy(t, dir)
	if err := os.WriteFile(policyPath, []byte(strings.Replace(signedPolicyFixture, "allow-read", "allow-read-tampered", 1)), 0o644); err != nil {
		t.Fatalf("tamper policy: %v", err)
	}
	opts.BypassSignature = true
	p, err := LoadPolicyFileWithOptions(policyPath, opts)
	if err != nil {
		t.Fatalf("LoadPolicyFileWithOptions with bypass: %v", err)
	}
	if p.Rules[0].ID != "allow-read-tampered" {
		t.Fatalf("expected tampered policy to load under bypass, got %q", p.Rules[0].ID)
	}
}

func TestLoadWithInheritanceWithOptions_RejectsTamperedParent(t *testing.T) {
	root := t.TempDir()
	policyPath, opts := writeSignedPolicy(t, root)
	child := filepath.Join(root, "sub")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("mkdir child: %v", err)
	}
	if err := os.WriteFile(policyPath, []byte(strings.Replace(signedPolicyFixture, "allow-read", "allow-read-tampered", 1)), 0o644); err != nil {
		t.Fatalf("tamper policy: %v", err)
	}
	_, _, err := LoadWithInheritanceWithOptions(child, opts)
	if err == nil || !IsPolicySignatureError(err) {
		t.Fatalf("expected inherited signature error, got %v", err)
	}
}
