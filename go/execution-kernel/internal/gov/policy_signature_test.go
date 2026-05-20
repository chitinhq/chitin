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

func TestLoadPolicyFile_AdvisoryModeIgnoresSignatureWithoutTrustKey(t *testing.T) {
	dir := t.TempDir()
	policyPath, _ := writeSignedPolicy(t, dir)
	if err := os.RemoveAll(filepath.Join(dir, ".chitin")); err != nil {
		t.Fatalf("remove trust dir: %v", err)
	}
	t.Setenv("CHITIN_POLICY_PUBLIC_KEY", "")
	t.Setenv("CHITIN_POLICY_TRUST_DIR", filepath.Join(dir, "missing-trust"))
	t.Setenv("CHITIN_POLICY_REQUIRE_SIGNATURE", "")

	p, err := LoadPolicyFile(policyPath)
	if err != nil {
		t.Fatalf("LoadPolicyFile advisory mode should not require an unconfigured trust key: %v", err)
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

// Operator-presence bypass: missing sidecar is normally
// policy_signature_missing, but when CHITIN_GOV_OPERATOR_AUTHORIZED=1
// is set the policy loads cleanly. This is the "operator is in the
// loop" trust path that lets a fresh worktree (which doesn't inherit
// the gitignored sidecar) work interactively without an explicit
// sidecar-copy step.
func TestVerifyPolicySignature_OperatorPresenceBypassesMissingSidecar(t *testing.T) {
	dir := t.TempDir()
	policyPath := filepath.Join(dir, "chitin.yaml")
	if err := os.WriteFile(policyPath, []byte(signedPolicyFixture), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	// Pin a trust dir so signatureRequired() would return true if the
	// bypass didn't fire — proves the bypass is doing the work.
	trustDir := filepath.Join(dir, ".chitin", "trust")
	if err := os.MkdirAll(trustDir, 0o755); err != nil {
		t.Fatalf("mkdir trust: %v", err)
	}
	pub, _, err := GeneratePolicyKeyPair()
	if err != nil {
		t.Fatalf("GeneratePolicyKeyPair: %v", err)
	}
	if err := os.WriteFile(filepath.Join(trustDir, DefaultPolicyPublicKey), []byte(pub), 0o644); err != nil {
		t.Fatalf("write trust key: %v", err)
	}
	opts := PolicyLoadOptions{TrustDir: trustDir}

	// Without the bypass: should fail with policy_signature_missing.
	os.Unsetenv("CHITIN_GOV_OPERATOR_AUTHORIZED")
	if err := VerifyPolicySignatureFile(policyPath, opts); err == nil {
		t.Fatal("expected policy_signature_missing without operator bypass")
	} else if !strings.Contains(err.Error(), "policy_signature_missing") {
		t.Fatalf("expected policy_signature_missing, got %v", err)
	}

	// With the bypass: should succeed.
	t.Setenv("CHITIN_GOV_OPERATOR_AUTHORIZED", "1")
	if err := VerifyPolicySignatureFile(policyPath, opts); err != nil {
		t.Fatalf("operator-presence bypass should allow missing sidecar, got %v", err)
	}
}

// Operator-presence bypass also short-circuits tampered-policy
// detection. This is intentional: an operator sitting in the loop can
// see what's happening; the sidecar mechanism is for the
// no-operator-watching autonomous mode (cron, swarm dispatch). The
// bypass mirrors PolicyLoadOptions.BypassSignature behavior.
func TestVerifyPolicySignature_OperatorPresenceShortCircuitsTamperCheck(t *testing.T) {
	dir := t.TempDir()
	policyPath, opts := writeSignedPolicy(t, dir)
	if err := os.WriteFile(policyPath, []byte(strings.Replace(signedPolicyFixture, "allow-read", "allow-read-tampered", 1)), 0o644); err != nil {
		t.Fatalf("tamper policy: %v", err)
	}

	t.Setenv("CHITIN_GOV_OPERATOR_AUTHORIZED", "1")
	if err := VerifyPolicySignatureFile(policyPath, opts); err != nil {
		t.Fatalf("operator-presence bypass should allow tampered policy, got %v", err)
	}
}

// Operator-presence bypass does NOT fire when env var is set to a
// non-"1" value. Guards against accidental truthy-ish settings.
func TestVerifyPolicySignature_BypassOnlyOnExactlyOne(t *testing.T) {
	dir := t.TempDir()
	policyPath := filepath.Join(dir, "chitin.yaml")
	if err := os.WriteFile(policyPath, []byte(signedPolicyFixture), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	trustDir := filepath.Join(dir, ".chitin", "trust")
	if err := os.MkdirAll(trustDir, 0o755); err != nil {
		t.Fatalf("mkdir trust: %v", err)
	}
	pub, _, err := GeneratePolicyKeyPair()
	if err != nil {
		t.Fatalf("GeneratePolicyKeyPair: %v", err)
	}
	if err := os.WriteFile(filepath.Join(trustDir, DefaultPolicyPublicKey), []byte(pub), 0o644); err != nil {
		t.Fatalf("write trust key: %v", err)
	}
	opts := PolicyLoadOptions{TrustDir: trustDir}

	for _, val := range []string{"true", "yes", "0", "", "TRUE"} {
		t.Setenv("CHITIN_GOV_OPERATOR_AUTHORIZED", val)
		if err := VerifyPolicySignatureFile(policyPath, opts); err == nil {
			t.Fatalf("CHITIN_GOV_OPERATOR_AUTHORIZED=%q should NOT bypass — value must be exactly %q", val, "1")
		}
	}
}
