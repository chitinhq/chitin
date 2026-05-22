package adapter

import (
	"fmt"
	"path/filepath"
)

// ConstitutionProjection is the kit-specific destination of a project's
// canonical constitution (FR-013). Each kit expects the project's governing
// principles in its own location — spec-kit reads `.specify/memory/
// constitution.md`. A projection names that location and the content to
// place there; applying it (writing the file) is a side effect for the
// caller, NOT this package: compilation here stays a pure transform.
type ConstitutionProjection struct {
	// Kit is the kit the projection targets.
	Kit string
	// RelPath is the repo-relative path the kit expects the constitution at.
	RelPath string
	// Content is the canonical constitution text to place at RelPath.
	Content string
}

// AbsPath returns RelPath joined onto repoPath — the absolute file a caller
// would write the projection to.
func (p *ConstitutionProjection) AbsPath(repoPath string) string {
	return filepath.Join(repoPath, p.RelPath)
}

// constitutionRelPaths is the per-kit expected location of the constitution.
// It is the single source of truth for ProjectConstitution; a kit with no
// entry has no defined constitution location and ProjectConstitution rejects
// it rather than guessing a path.
var constitutionRelPaths = map[string]string{
	"speckit":  filepath.Join(".specify", "memory", "constitution.md"),
	"openspec": filepath.Join("openspec", "memory", "constitution.md"),
}

// ProjectConstitution computes where a kit expects the canonical constitution
// and pairs it with the supplied canonical content (FR-013). It is a pure
// function: it returns a ConstitutionProjection describing the destination
// and content; it writes nothing. A caller — outside this side-effect-free
// layer — performs the actual file write.
//
// It returns an error for a kit with no defined constitution location, so the
// projection path is never guessed.
func ProjectConstitution(kit, canonicalContent string) (*ConstitutionProjection, error) {
	rel, ok := constitutionRelPaths[kit]
	if !ok {
		return nil, fmt.Errorf("adapter: no constitution location defined for kit %q", kit)
	}
	return &ConstitutionProjection{Kit: kit, RelPath: rel, Content: canonicalContent}, nil
}
