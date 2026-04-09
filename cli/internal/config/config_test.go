package config

import "testing"

func TestDefaultsRefinementDisabled(t *testing.T) {
	cfg := Defaults()
	if cfg.Features.Refinement {
		t.Fatal("expected Features.Refinement to default to false")
	}
	if cfg.RefinementEnabled() {
		t.Fatal("expected RefinementEnabled() to return false on defaults")
	}
}

func TestRefinementEnabledHelper(t *testing.T) {
	cfg := Defaults()
	cfg.Features.Refinement = true
	if !cfg.RefinementEnabled() {
		t.Fatal("expected RefinementEnabled() to return true when flag set")
	}
}
