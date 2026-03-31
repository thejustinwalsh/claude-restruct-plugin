package toggle

import (
	"testing"
)

func TestEnabledByDefault(t *testing.T) {
	dir := t.TempDir()
	if !IsEnabled(dir) {
		t.Fatal("should be enabled by default (no sentinel)")
	}
}

func TestDisableAndEnable(t *testing.T) {
	dir := t.TempDir()

	if err := Disable(dir); err != nil {
		t.Fatal(err)
	}
	if IsEnabled(dir) {
		t.Fatal("should be disabled after Disable()")
	}

	if err := Enable(dir); err != nil {
		t.Fatal(err)
	}
	if !IsEnabled(dir) {
		t.Fatal("should be enabled after Enable()")
	}
}

func TestEnableWhenAlreadyEnabled(t *testing.T) {
	dir := t.TempDir()
	// Should not error when sentinel doesn't exist
	if err := Enable(dir); err != nil {
		t.Fatal(err)
	}
}
