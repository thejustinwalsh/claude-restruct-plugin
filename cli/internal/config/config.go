package config

import (
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Ollama     OllamaConfig     `mapstructure:"ollama"`
	Refinement RefinementConfig `mapstructure:"refinement"`
	Cache      CacheConfig      `mapstructure:"cache"`
	Rules      RulesConfig      `mapstructure:"rules"`
}

type OllamaConfig struct {
	URL        string        `mapstructure:"url"`
	Model      string        `mapstructure:"model"`
	Timeout    time.Duration `mapstructure:"timeout"`
	KeepAlive  time.Duration `mapstructure:"keep_alive"`
	MinVersion string        `mapstructure:"min_version"`
}

type RefinementConfig struct {
	Temperature float64 `mapstructure:"temperature"`
	MaxTokens   int     `mapstructure:"max_tokens"`
	MinWords    int     `mapstructure:"min_words"`
}

type CacheConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Dir     string `mapstructure:"dir"`
}

type RulesConfig struct {
	Files []string `mapstructure:"files"`
}

func Defaults() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		Ollama: OllamaConfig{
			URL:        "http://localhost:11434",
			Model:      "qwen2.5-coder:14b",
			Timeout:    10 * time.Second,
			KeepAlive:  60 * time.Minute,
			MinVersion: "0.18.0",
		},
		Refinement: RefinementConfig{
			Temperature: 0.3,
			MaxTokens:   2048,
			MinWords:    5,
		},
		Cache: CacheConfig{
			Enabled: true,
			Dir:     filepath.Join(home, ".cache", "restruct"),
		},
		Rules: RulesConfig{
			Files: []string{"agents.md", "CLAUDE.md", ".claude/rules.md"},
		},
	}
}

func LoadFromViper() (*Config, error) {
	cfg := Defaults()

	// Set defaults in viper so they get picked up
	viper.SetDefault("ollama.url", cfg.Ollama.URL)
	viper.SetDefault("ollama.model", cfg.Ollama.Model)
	viper.SetDefault("ollama.timeout", cfg.Ollama.Timeout)
	viper.SetDefault("ollama.keep_alive", cfg.Ollama.KeepAlive)
	viper.SetDefault("ollama.min_version", cfg.Ollama.MinVersion)
	viper.SetDefault("refinement.temperature", cfg.Refinement.Temperature)
	viper.SetDefault("refinement.max_tokens", cfg.Refinement.MaxTokens)
	viper.SetDefault("refinement.min_words", cfg.Refinement.MinWords)
	viper.SetDefault("cache.enabled", cfg.Cache.Enabled)
	viper.SetDefault("cache.dir", cfg.Cache.Dir)
	viper.SetDefault("rules.files", cfg.Rules.Files)

	if err := viper.Unmarshal(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
