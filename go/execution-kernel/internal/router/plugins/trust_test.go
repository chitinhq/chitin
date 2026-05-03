package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrustPolicy_Off(t *testing.T) {
	tp := TrustPolicy{Mode: "off"}
	err := tp.Verify(PluginManifest{Name: "any", Module: "/anything.py"})
	if err != nil {
		t.Errorf("off mode rejected plugin: %v", err)
	}
}

func TestTrustPolicy_PathPass(t *testing.T) {
	dir := t.TempDir()
	plug := filepath.Join(dir, "p.py")
	if err := os.WriteFile(plug, []byte("# plugin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tp := TrustPolicy{Mode: "path", TrustedPaths: []string{plug}}
	if err := tp.Verify(PluginManifest{Name: "p", Module: plug}); err != nil {
		t.Errorf("path mode rejected trusted plugin: %v", err)
	}
}

func TestTrustPolicy_PathReject(t *testing.T) {
	tp := TrustPolicy{Mode: "path", TrustedPaths: []string{"/some/other/p.py"}}
	err := tp.Verify(PluginManifest{Name: "evil", Module: "/tmp/evil.py"})
	if err == nil {
		t.Error("path mode allowed untrusted plugin")
	}
	if !strings.Contains(err.Error(), "trusted_paths") {
		t.Errorf("error doesn't mention trusted_paths: %v", err)
	}
}

func TestTrustPolicy_HashPass(t *testing.T) {
	dir := t.TempDir()
	plug := filepath.Join(dir, "p.py")
	body := []byte("# plugin\n")
	if err := os.WriteFile(plug, body, 0o644); err != nil {
		t.Fatal(err)
	}
	hash, err := HashFile(plug)
	if err != nil {
		t.Fatal(err)
	}
	tp := TrustPolicy{Mode: "hash", TrustedHashes: map[string]string{"p": hash}}
	if err := tp.Verify(PluginManifest{Name: "p", Module: plug}); err != nil {
		t.Errorf("hash mode rejected trusted plugin: %v", err)
	}
}

func TestTrustPolicy_HashTamperDetected(t *testing.T) {
	dir := t.TempDir()
	plug := filepath.Join(dir, "p.py")
	if err := os.WriteFile(plug, []byte("# original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	originalHash, _ := HashFile(plug)
	// Tamper with the file
	if err := os.WriteFile(plug, []byte("# evil tampered version\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tp := TrustPolicy{Mode: "hash", TrustedHashes: map[string]string{"p": originalHash}}
	err := tp.Verify(PluginManifest{Name: "p", Module: plug})
	if err == nil {
		t.Error("hash mode allowed tampered plugin")
	}
	if !strings.Contains(err.Error(), "hash mismatch") {
		t.Errorf("error doesn't mention hash mismatch: %v", err)
	}
}

func TestTrustPolicy_HashMissingDeclaration(t *testing.T) {
	dir := t.TempDir()
	plug := filepath.Join(dir, "p.py")
	if err := os.WriteFile(plug, []byte("# plugin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tp := TrustPolicy{Mode: "hash", TrustedHashes: map[string]string{"other": "abc"}}
	err := tp.Verify(PluginManifest{Name: "p", Module: plug})
	if err == nil {
		t.Error("hash mode allowed plugin with no declared hash")
	}
	if !strings.Contains(err.Error(), "no trusted_hash") {
		t.Errorf("error doesn't mention missing declaration: %v", err)
	}
}

func TestTrustPolicy_PathPlusHash(t *testing.T) {
	dir := t.TempDir()
	plug := filepath.Join(dir, "p.py")
	body := []byte("# plugin\n")
	if err := os.WriteFile(plug, body, 0o644); err != nil {
		t.Fatal(err)
	}
	hash, _ := HashFile(plug)
	tp := TrustPolicy{
		Mode:           "path+hash",
		TrustedPaths:   []string{plug},
		TrustedHashes:  map[string]string{"p": hash},
	}
	if err := tp.Verify(PluginManifest{Name: "p", Module: plug}); err != nil {
		t.Errorf("path+hash mode rejected trusted plugin: %v", err)
	}
	// Tamper → should fail
	_ = os.WriteFile(plug, []byte("# evil\n"), 0o644)
	err := tp.Verify(PluginManifest{Name: "p", Module: plug})
	if err == nil {
		t.Error("path+hash mode allowed tampered plugin")
	}
}

func TestHashFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x")
	if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := HashFile(p)
	if err != nil {
		t.Fatal(err)
	}
	// SHA-256("hello") = 2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
	want := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if h != want {
		t.Errorf("HashFile(\"hello\")=%q want %q", h, want)
	}
}
