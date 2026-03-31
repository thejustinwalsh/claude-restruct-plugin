package permit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_Defaults(t *testing.T) {
	cfg, err := LoadConfig("/nonexistent/path")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AutoApproveWrites {
		t.Error("AutoApproveWrites should default to true")
	}
	if len(cfg.TrustedURLs) == 0 {
		t.Error("TrustedURLs should have defaults")
	}
	if cfg.AgentReview.Timeout != 30 {
		t.Errorf("AgentReview.Timeout = %d, want 30", cfg.AgentReview.Timeout)
	}
}

func TestLoadConfig_FromFile(t *testing.T) {
	dir := t.TempDir()
	restructDir := filepath.Join(dir, ".restruct")
	os.MkdirAll(restructDir, 0755)

	yaml := `
allowed_paths:
  - ../shared
  - /opt/data
trusted_urls:
  - "https://example.com/*"
blocked_urls:
  - "https://evil.com/*"
auto_approve_writes: false
sensitive_env_patterns:
  - "CORP_*"
always_ask:
  - "Bash(rm -rf *)"
agent_review:
  enabled: true
  timeout: 10
  triggers:
    - network_with_query_params
`
	os.WriteFile(filepath.Join(restructDir, "permissions.yaml"), []byte(yaml), 0644)

	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.AutoApproveWrites {
		t.Error("AutoApproveWrites should be false from file")
	}
	if len(cfg.AllowedPaths) != 2 {
		t.Errorf("AllowedPaths = %d, want 2", len(cfg.AllowedPaths))
	}
	if len(cfg.TrustedURLs) != 1 {
		t.Errorf("TrustedURLs = %d, want 1 (overridden)", len(cfg.TrustedURLs))
	}
	if !cfg.AgentReview.Enabled {
		t.Error("AgentReview.Enabled should be true")
	}
	if len(cfg.SensitiveEnvPatterns) != 1 || cfg.SensitiveEnvPatterns[0] != "CORP_*" {
		t.Errorf("SensitiveEnvPatterns = %v, want [CORP_*]", cfg.SensitiveEnvPatterns)
	}
}

func TestLoadConfig_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	restructDir := filepath.Join(dir, ".restruct")
	os.MkdirAll(restructDir, 0755)
	os.WriteFile(filepath.Join(restructDir, "permissions.yaml"), []byte(":::invalid"), 0644)

	_, err := LoadConfig(dir)
	if err == nil {
		t.Error("expected error for malformed YAML")
	}
}
