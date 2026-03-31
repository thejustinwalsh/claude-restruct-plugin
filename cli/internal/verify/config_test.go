package verify

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigPath(t *testing.T) {
	got := ConfigPath("/project")
	want := filepath.Join("/project", ".restruct", "verify.yaml")
	if got != want {
		t.Errorf("ConfigPath = %q, want %q", got, want)
	}
}

func TestLoadConfig_NotFound(t *testing.T) {
	cfg, err := LoadConfig(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Fatal("expected nil config when file missing")
	}
}

func TestLoadConfig_Valid(t *testing.T) {
	dir := t.TempDir()
	restructDir := filepath.Join(dir, ".restruct")
	os.MkdirAll(restructDir, 0755)

	content := `checks:
  - name: test
    command: "pnpm test"
  - name: typecheck
    command: "pnpm --filter web exec tsc --noEmit"
    globs:
      - "web/**/*.ts"
      - "web/**/*.tsx"
`
	os.WriteFile(filepath.Join(restructDir, "verify.yaml"), []byte(content), 0644)

	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if len(cfg.Checks) != 2 {
		t.Fatalf("expected 2 checks, got %d", len(cfg.Checks))
	}
	if cfg.Checks[0].Name != "test" {
		t.Errorf("check[0].Name = %q, want %q", cfg.Checks[0].Name, "test")
	}
	if cfg.Checks[1].Command != "pnpm --filter web exec tsc --noEmit" {
		t.Errorf("check[1].Command = %q", cfg.Checks[1].Command)
	}
	if len(cfg.Checks[1].Globs) != 2 {
		t.Errorf("check[1].Globs len = %d, want 2", len(cfg.Checks[1].Globs))
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	restructDir := filepath.Join(dir, ".restruct")
	os.MkdirAll(restructDir, 0755)
	os.WriteFile(filepath.Join(restructDir, "verify.yaml"), []byte("not: [valid: yaml"), 0644)

	_, err := LoadConfig(dir)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestSaveAndLoadConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := &VerifyConfig{
		Checks: []CheckConfig{
			{Name: "lint", Command: "eslint .", Globs: []string{"**/*.ts"}},
			{Name: "build", Command: "make build"},
		},
	}

	if err := SaveConfig(dir, cfg); err != nil {
		t.Fatalf("SaveConfig error: %v", err)
	}

	loaded, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	if len(loaded.Checks) != 2 {
		t.Fatalf("expected 2 checks, got %d", len(loaded.Checks))
	}
	if loaded.Checks[0].Name != "lint" {
		t.Errorf("check[0].Name = %q, want %q", loaded.Checks[0].Name, "lint")
	}
	if loaded.Checks[0].Globs[0] != "**/*.ts" {
		t.Errorf("check[0].Globs[0] = %q", loaded.Checks[0].Globs[0])
	}
	if loaded.Checks[1].Command != "make build" {
		t.Errorf("check[1].Command = %q", loaded.Checks[1].Command)
	}
}
