package permit

import (
	"errors"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config represents the .restruct/permissions.yaml configuration.
type Config struct {
	AllowedPaths         []string          `yaml:"allowed_paths"`
	TrustedURLs          []string          `yaml:"trusted_urls"`
	BlockedURLs          []string          `yaml:"blocked_urls"`
	AutoApproveWrites    bool              `yaml:"auto_approve_writes"`
	SensitiveEnvPatterns []string          `yaml:"sensitive_env_patterns"`
	AlwaysAsk            []string          `yaml:"always_ask"`
	AgentReview          AgentReviewConfig `yaml:"agent_review"`
}

// AgentReviewConfig controls the optional LLM-based review for borderline cases.
type AgentReviewConfig struct {
	Enabled  bool     `yaml:"enabled"`
	Timeout  int      `yaml:"timeout"`
	Triggers []string `yaml:"triggers"`
}

// ConfigPath returns the path to permissions.yaml for a project directory.
func ConfigPath(projectDir string) string {
	return filepath.Join(projectDir, ".restruct", "permissions.yaml")
}

// LoadConfig reads .restruct/permissions.yaml from the given project directory.
// Returns Defaults() if the file does not exist (graceful degradation).
func LoadConfig(projectDir string) (*Config, error) {
	data, err := os.ReadFile(ConfigPath(projectDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Defaults(), nil
		}
		return nil, err
	}

	cfg := Defaults()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Defaults returns a Config with sensible defaults for projects without
// a permissions.yaml file. Auto-approves writes inside project root,
// trusts common package registries and localhost.
func Defaults() *Config {
	return &Config{
		AutoApproveWrites: true,
		TrustedURLs: []string{
			"https://registry.npmjs.org/*",
			"https://pypi.org/*",
			"http://localhost:*",
			"http://127.0.0.1:*",
		},
		AgentReview: AgentReviewConfig{
			Enabled: false,
			Timeout: 30,
		},
	}
}
