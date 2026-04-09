package config

import (
	"errors"
	"strings"
	"testing"
)

func TestGuardRefinementDisabled(t *testing.T) {
	cfg := Defaults()
	err := GuardRefinement(cfg)
	if err == nil {
		t.Fatal("expected error when refinement disabled")
	}
	if !errors.Is(err, ErrRefinementDisabled) {
		t.Fatalf("expected ErrRefinementDisabled, got %v", err)
	}
	if !strings.Contains(err.Error(), "features.refinement: true") {
		t.Fatalf("expected error message to hint at config key, got %q", err.Error())
	}
}

func TestGuardRefinementEnabled(t *testing.T) {
	cfg := Defaults()
	cfg.Features.Refinement = true
	if err := GuardRefinement(cfg); err != nil {
		t.Fatalf("expected nil when refinement enabled, got %v", err)
	}
}
