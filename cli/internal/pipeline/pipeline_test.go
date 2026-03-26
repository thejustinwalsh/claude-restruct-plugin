package pipeline

import (
	"testing"

	"github.com/tjw/restruct/internal/config"
)

func TestNewPipeline(t *testing.T) {
	cfg := config.Defaults()
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil pipeline")
	}
}
