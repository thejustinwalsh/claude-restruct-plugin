package config

import (
	"testing"

	"github.com/spf13/viper"
)

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

func TestLoadFromViperRespectsFeaturesRefinement(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	viper.Set("features.refinement", true)

	cfg, err := LoadFromViper()
	if err != nil {
		t.Fatalf("LoadFromViper: %v", err)
	}
	if !cfg.RefinementEnabled() {
		t.Fatal("expected RefinementEnabled() true when viper has features.refinement=true")
	}
}

func TestLoadFromViperDefaultsToDisabled(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	cfg, err := LoadFromViper()
	if err != nil {
		t.Fatalf("LoadFromViper: %v", err)
	}
	if cfg.RefinementEnabled() {
		t.Fatal("expected RefinementEnabled() false by default")
	}
}
