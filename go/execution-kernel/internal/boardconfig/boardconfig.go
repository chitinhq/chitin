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

func ResolveField(slug, field string) (string, error) {
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
	if value := strings.TrimSpace(os.Getenv(spec.EnvVar)); value != "" {
		return value, nil
	}
	if value := strings.TrimSpace(config[field]); value != "" {
		return value, nil
	}
	if spec.DefaultValue != "" {
		return spec.DefaultValue, nil
	}
	if spec.Required {
		return "", MissingFieldError{Field: field}
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
