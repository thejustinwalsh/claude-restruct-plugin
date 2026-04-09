package config

import (
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/viper"
)

type FeaturesConfig struct {
	Refinement bool `mapstructure:"refinement"`
}

type Config struct {
	Features   FeaturesConfig   `mapstructure:"features"`
	Ollama     OllamaConfig     `mapstructure:"ollama"`
	Refinement RefinementConfig `mapstructure:"refinement"`
	Cache      CacheConfig      `mapstructure:"cache"`
	Rules      RulesConfig      `mapstructure:"rules"`
	Server     ServerConfig     `mapstructure:"server"`
}

type ServerConfig struct {
	Port string `mapstructure:"port"`
}

type OllamaConfig struct {
	URL            string        `mapstructure:"url"`
	Model          string        `mapstructure:"model"`
	ConnectTimeout time.Duration `mapstructure:"connect_timeout"` // fast: is Ollama reachable?
	RequestTimeout time.Duration `mapstructure:"request_timeout"` // generous: total LLM response time
	StallTimeout   time.Duration `mapstructure:"stall_timeout"`   // no tokens for this long = abort
	KeepAlive      time.Duration `mapstructure:"keep_alive"`
	MinVersion     string        `mapstructure:"min_version"`

	// Deprecated: use ConnectTimeout instead. Kept for backward compat with existing configs.
	Timeout time.Duration `mapstructure:"timeout"`
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
		Features: FeaturesConfig{Refinement: false},
		Ollama: OllamaConfig{
			URL:            "http://localhost:11434",
			Model:          "qwen2.5-coder:14b",
			ConnectTimeout: 5 * time.Second,
			RequestTimeout: 120 * time.Second,
			StallTimeout:   30 * time.Second,
			KeepAlive:      60 * time.Minute,
			MinVersion:     "0.18.0",
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
		Server: ServerConfig{
			Port: "8377",
		},
	}
}

func LoadFromViper() (*Config, error) {
	cfg := Defaults()

	// Set defaults in viper so they get picked up
	viper.SetDefault("ollama.url", cfg.Ollama.URL)
	viper.SetDefault("ollama.model", cfg.Ollama.Model)
	viper.SetDefault("ollama.connect_timeout", cfg.Ollama.ConnectTimeout)
	viper.SetDefault("ollama.request_timeout", cfg.Ollama.RequestTimeout)
	viper.SetDefault("ollama.stall_timeout", cfg.Ollama.StallTimeout)
	viper.SetDefault("ollama.keep_alive", cfg.Ollama.KeepAlive)
	viper.SetDefault("ollama.min_version", cfg.Ollama.MinVersion)
	viper.SetDefault("refinement.temperature", cfg.Refinement.Temperature)
	viper.SetDefault("refinement.max_tokens", cfg.Refinement.MaxTokens)
	viper.SetDefault("refinement.min_words", cfg.Refinement.MinWords)
	viper.SetDefault("cache.enabled", cfg.Cache.Enabled)
	viper.SetDefault("cache.dir", cfg.Cache.Dir)
	viper.SetDefault("rules.files", cfg.Rules.Files)
	viper.SetDefault("server.port", cfg.Server.Port)

	if err := viper.Unmarshal(cfg); err != nil {
		return nil, err
	}

	// Backward compat: if old "timeout" is set but new fields aren't, use it
	if cfg.Ollama.Timeout > 0 && cfg.Ollama.ConnectTimeout == 5*time.Second {
		cfg.Ollama.ConnectTimeout = cfg.Ollama.Timeout
	}

	return cfg, nil
}

// RefinementEnabled reports whether prompt refinement is enabled in this release.
// When false, the refine hook short-circuits to passthrough and toggle commands refuse.
func (c *Config) RefinementEnabled() bool {
	return c.Features.Refinement
}
