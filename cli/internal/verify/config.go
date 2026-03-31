package verify

import (
	"errors"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// CheckConfig represents a single verification check.
type CheckConfig struct {
	Name    string   `yaml:"name"`
	Command string   `yaml:"command"`
	Globs   []string `yaml:"globs,omitempty"`
}

// VerifyConfig is the top-level structure of .restruct/verify.yaml.
type VerifyConfig struct {
	Checks []CheckConfig `yaml:"checks"`
}

// ConfigPath returns the path to verify.yaml for a project directory.
func ConfigPath(projectDir string) string {
	return filepath.Join(projectDir, ".restruct", "verify.yaml")
}

// LoadConfig reads .restruct/verify.yaml from the given project directory.
// Returns nil, nil if the file does not exist (graceful degradation).
func LoadConfig(projectDir string) (*VerifyConfig, error) {
	data, err := os.ReadFile(ConfigPath(projectDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var cfg VerifyConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// SaveConfig writes the verify config to .restruct/verify.yaml.
func SaveConfig(projectDir string, cfg *VerifyConfig) error {
	dir := filepath.Join(projectDir, ".restruct")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(ConfigPath(projectDir), data, 0644)
}
