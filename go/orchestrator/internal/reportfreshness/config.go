package reportfreshness

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	DefaultCadenceMinutes          = 360
	DefaultEscalationCooldownHours = 24
)

//go:embed default-config.yaml
var defaultConfigYAML []byte

var ErrConfigMissing = errors.New("report freshness config missing")

type Config struct {
	Paths                   []WatchedPath `yaml:"paths" json:"paths"`
	CadenceMinutes          int           `yaml:"cadence_minutes" json:"cadence_minutes"`
	EscalationCooldownHours int           `yaml:"escalation_cooldown_hours" json:"escalation_cooldown_hours"`
}

type WatchedPath struct {
	Path     string `yaml:"path" json:"path"`
	SLAHours int    `yaml:"sla_hours" json:"sla_hours"`
}

func DefaultConfigPath() string {
	if p := os.Getenv("CHITIN_REPORT_FRESHNESS_CONFIG"); p != "" {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".chitin", "report-freshness.yaml")
	}
	return filepath.Join(".chitin", "report-freshness.yaml")
}

func LoadConfig(path string) (Config, error) {
	if path == "" {
		path = DefaultConfigPath()
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, fmt.Errorf("%w: %s", ErrConfigMissing, path)
		}
		return Config{}, err
	}
	return parseConfig(body)
}

func LoadDefaultConfig() (Config, error) {
	return parseConfig(defaultConfigYAML)
}

func LoadConfigOrDefault(path string) (Config, error) {
	cfg, err := LoadConfig(path)
	if err == nil {
		return cfg, nil
	}
	if !errors.Is(err, ErrConfigMissing) {
		return Config{}, err
	}
	return LoadDefaultConfig()
}

func parseConfig(body []byte) (Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(body, &cfg); err != nil {
		return Config{}, err
	}
	if cfg.CadenceMinutes == 0 {
		cfg.CadenceMinutes = DefaultCadenceMinutes
	}
	if cfg.EscalationCooldownHours == 0 {
		cfg.EscalationCooldownHours = DefaultEscalationCooldownHours
	}
	for i := range cfg.Paths {
		if cfg.Paths[i].SLAHours <= 0 {
			return Config{}, fmt.Errorf("paths[%d].sla_hours must be positive", i)
		}
		if cfg.Paths[i].Path == "" {
			return Config{}, fmt.Errorf("paths[%d].path is required", i)
		}
	}
	return cfg, nil
}
