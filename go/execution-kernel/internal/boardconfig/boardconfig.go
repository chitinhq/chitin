package boardconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type FieldSpec struct {
	EnvVar       string
	Required     bool
	DefaultValue string
}

var fieldSpecs = map[string]FieldSpec{
	"repo": {
		EnvVar:   "KANBAN_BOARD_REPO",
		Required: true,
	},
	"default_branch": {
		EnvVar:   "KANBAN_BOARD_DEFAULT_BRANCH",
		Required: true,
	},
	"workspace_root": {
		EnvVar:   "KANBAN_BOARD_WORKSPACE_ROOT",
		Required: true,
	},
	"kernel_bin": {
		EnvVar:   "KANBAN_BOARD_KERNEL_BIN",
		Required: true,
	},
	"chitin_yaml": {
		EnvVar:       "KANBAN_BOARD_CHITIN_YAML",
		DefaultValue: "chitin.yaml",
	},
	"soul_map": {
		EnvVar:       "KANBAN_BOARD_SOUL_MAP",
		DefaultValue: `{"correctness":"knuth","architecture":"davinci","dispatch":"sun-tzu","research":"socrates","default":"sun-tzu"}`,
	},
	"chitin_db_path": {
		// Default is computed per-slug in ResolveField since it
		// depends on $HOME and the board slug.
		EnvVar: "KANBAN_BOARD_CHITIN_DB_PATH",
	},
}

var requiredFields = []string{
	"repo",
	"default_branch",
	"workspace_root",
	"kernel_bin",
}

var ErrNoBoardsInitialized = errors.New("no boards initialized")

type UnknownBoardError struct {
	Slug string
}

func (e UnknownBoardError) Error() string {
	return fmt.Sprintf("unknown board: %s", e.Slug)
}

type UnknownFieldError struct {
	Field string
}

func (e UnknownFieldError) Error() string {
	return fmt.Sprintf("unknown field: %s", e.Field)
}

type MissingFieldError struct {
	Field string
}

func (e MissingFieldError) Error() string {
	return fmt.Sprintf("missing field: %s", e.Field)
}

type MissingConfigError struct {
	Path string
}

func (e MissingConfigError) Error() string {
	return fmt.Sprintf("missing config: %s", e.Path)
}

type InvalidSlugError struct {
	Slug string
}

func (e InvalidSlugError) Error() string {
	return fmt.Sprintf("invalid board slug: %q", e.Slug)
}

// validateSlug rejects slugs that would let a caller escape the boards
// root. We forbid empty values, path separators, and `.`/`..` because
// they would resolve to a sibling or parent directory once joined with
// the boards root.
func validateSlug(slug string) error {
	if slug == "" {
		return InvalidSlugError{Slug: slug}
	}
	if slug == "." || slug == ".." {
		return InvalidSlugError{Slug: slug}
	}
	if strings.ContainsAny(slug, `/\`) {
		return InvalidSlugError{Slug: slug}
	}
	if strings.ContainsRune(slug, 0) {
		return InvalidSlugError{Slug: slug}
	}
	return nil
}

// expandHome replaces a leading `~/` (or bare `~`) with the operator's
// home directory. Other tilde forms (e.g. `~user/...`) are left alone
// since they aren't part of the board-config contract.
func expandHome(value string) (string, error) {
	if value == "" || (value[0] != '~') {
		return value, nil
	}
	if value != "~" && !strings.HasPrefix(value, "~/") {
		return value, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user home: %w", err)
	}
	if value == "~" {
		return home, nil
	}
	return filepath.Join(home, value[2:]), nil
}

// tildeExpandableFields lists fields whose value is a filesystem path
// the operator may write with a leading `~/`. We expand those at read
// time so downstream tools see an absolute path.
var tildeExpandableFields = map[string]struct{}{
	"workspace_root": {},
	"chitin_db_path": {},
}

func ResolveField(slug, field string) (string, error) {
	if err := validateSlug(slug); err != nil {
		return "", err
	}
	spec, ok := fieldSpecs[field]
	if !ok {
		return "", UnknownFieldError{Field: field}
	}
	config, err := loadConfig(slug)
	if err != nil {
		return "", err
	}
	if err := validateRequiredFields(config); err != nil {
		return "", err
	}
	value := strings.TrimSpace(os.Getenv(spec.EnvVar))
	if value == "" {
		value = strings.TrimSpace(config[field])
	}
	if value == "" && spec.DefaultValue != "" {
		value = spec.DefaultValue
	}
	if value == "" {
		if dynamic, err := dynamicDefault(field, slug); err != nil {
			return "", err
		} else if dynamic != "" {
			value = dynamic
		}
	}
	if value == "" {
		if spec.Required {
			return "", MissingFieldError{Field: field}
		}
		return "", nil
	}
	if _, expand := tildeExpandableFields[field]; expand {
		return expandHome(value)
	}
	return value, nil
}

// dynamicDefault returns the default value for fields whose default
// depends on the runtime environment (e.g. $HOME) or the board slug.
// Returns "" for fields without a dynamic default.
func dynamicDefault(field, slug string) (string, error) {
	switch field {
	case "chitin_db_path":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("user home: %w", err)
		}
		return filepath.Join(home, ".chitin", "kanban", slug, "kanban.db"), nil
	}
	return "", nil
}

func loadConfig(slug string) (map[string]string, error) {
	root, err := boardsRoot()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNoBoardsInitialized
		}
		return nil, fmt.Errorf("read boards dir: %w", err)
	}

	hasBoardDir := false
	for _, entry := range entries {
		if entry.IsDir() {
			hasBoardDir = true
			break
		}
	}
	if !hasBoardDir {
		return nil, ErrNoBoardsInitialized
	}

	boardDir := filepath.Join(root, slug)
	info, err := os.Stat(boardDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, UnknownBoardError{Slug: slug}
		}
		return nil, fmt.Errorf("stat board dir: %w", err)
	}
	if !info.IsDir() {
		return nil, UnknownBoardError{Slug: slug}
	}

	configPath := filepath.Join(boardDir, "config.json")
	raw, err := os.ReadFile(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, MissingConfigError{Path: configPath}
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	var config map[string]string
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return config, nil
}

func boardsRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user home: %w", err)
	}
	return filepath.Join(home, ".hermes", "kanban", "boards"), nil
}

func validateRequiredFields(config map[string]string) error {
	for _, field := range requiredFields {
		spec := fieldSpecs[field]
		if strings.TrimSpace(config[field]) == "" && strings.TrimSpace(os.Getenv(spec.EnvVar)) == "" {
			return MissingFieldError{Field: field}
		}
	}
	return nil
}
