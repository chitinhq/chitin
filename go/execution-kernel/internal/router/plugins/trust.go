package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// TrustPolicy declares the operator's allowlist of trusted plugin
// paths + content hashes. Read from chitin.yaml's
// `router.plugins_trust` section. Any plugin not on the allowlist
// is REJECTED at load time — protects against plugin tampering
// (file replaced after operator review) AND accidental loading of
// arbitrary scripts.
//
// Modes:
//   - "off" (default if section missing): no trust check; any
//     declared plugin runs. Useful for dev; operators should opt
//     into a stricter mode for production.
//   - "path": plugin's `module:` path must appear in TrustedPaths.
//     Tampering with file CONTENT goes undetected; protects only
//     against arbitrary new plugin paths sneaking in.
//   - "hash": plugin's SHA-256 must appear in TrustedHashes.
//     Tampering OR path-swap caught.
//   - "path+hash" (strictest): both checks must pass.
type TrustPolicy struct {
	Mode           string            `yaml:"mode" json:"mode"` // off | path | hash | path+hash
	TrustedPaths   []string          `yaml:"trusted_paths,omitempty" json:"trusted_paths,omitempty"`
	TrustedHashes  map[string]string `yaml:"trusted_hashes,omitempty" json:"trusted_hashes,omitempty"`
}

// Verify returns nil if the plugin manifest passes the trust
// policy, or a descriptive error if rejected.
func (tp TrustPolicy) Verify(manifest PluginManifest) error {
	if tp.Mode == "" || tp.Mode == "off" {
		return nil
	}

	pathOK := false
	if tp.Mode == "path" || tp.Mode == "path+hash" {
		// Match by absolute path so a relative module: doesn't slip
		// past via cwd manipulation
		abs, err := filepath.Abs(manifest.Module)
		if err != nil {
			return fmt.Errorf("plugins.trust: abs path: %w", err)
		}
		for _, p := range tp.TrustedPaths {
			pAbs, err := filepath.Abs(p)
			if err != nil {
				continue
			}
			if pAbs == abs {
				pathOK = true
				break
			}
		}
		if !pathOK {
			return fmt.Errorf(
				"plugins.trust: %s rejected — path %q not in trusted_paths (mode=%s)",
				manifest.Name, abs, tp.Mode,
			)
		}
	}

	if tp.Mode == "hash" || tp.Mode == "path+hash" {
		actual, err := HashFile(manifest.Module)
		if err != nil {
			return fmt.Errorf("plugins.trust: hash %s: %w", manifest.Module, err)
		}
		expected, declared := tp.TrustedHashes[manifest.Name]
		if !declared {
			return fmt.Errorf(
				"plugins.trust: %s rejected — no trusted_hash declared for plugin name (mode=%s)",
				manifest.Name, tp.Mode,
			)
		}
		if actual != expected {
			return fmt.Errorf(
				"plugins.trust: %s rejected — content hash mismatch (got %s, want %s, mode=%s)",
				manifest.Name, actual[:12], expected[:12], tp.Mode,
			)
		}
	}

	return nil
}

// HashFile returns the SHA-256 hex digest of a file's contents.
func HashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
