package verify

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestFilterChecks_NoChangedFiles(t *testing.T) {
	checks := []CheckConfig{
		{Name: "test", Command: "go test"},
	}
	filtered := FilterChecks(checks, nil)
	if len(filtered) != 0 {
		t.Errorf("expected no checks for no changed files, got %d", len(filtered))
	}
}

func TestFilterChecks_UnglobbedAlwaysMatches(t *testing.T) {
	checks := []CheckConfig{
		{Name: "test", Command: "go test"},
	}
	filtered := FilterChecks(checks, []string{"main.go"})
	if len(filtered) != 1 {
		t.Errorf("expected unglobbed check to match, got %d", len(filtered))
	}
}

func TestFilterChecks_GlobFiltering(t *testing.T) {
	checks := []CheckConfig{
		{Name: "typecheck", Command: "tsc", Globs: []string{"**/*.ts", "**/*.tsx"}},
		{Name: "go-vet", Command: "go vet", Globs: []string{"**/*.go"}},
		{Name: "test", Command: "pnpm test"}, // no globs
	}

	// Only Go files changed
	filtered := FilterChecks(checks, []string{"cli/main.go"})
	names := make(map[string]bool)
	for _, c := range filtered {
		names[c.Name] = true
	}

	if !names["go-vet"] {
		t.Error("expected go-vet to match .go files")
	}
	if !names["test"] {
		t.Error("expected unglobbed test to match")
	}
	if names["typecheck"] {
		t.Error("typecheck should not match .go files")
	}
}

func TestFilterChecks_DirPrefixedGlobs(t *testing.T) {
	checks := []CheckConfig{
		{Name: "typecheck", Command: "tsc", Globs: []string{"web/**/*.ts"}},
	}

	filtered := FilterChecks(checks, []string{"web/src/app.ts"})
	if len(filtered) != 1 {
		t.Errorf("expected web/**/*.ts to match web/src/app.ts, got %d matches", len(filtered))
	}

	filtered = FilterChecks(checks, []string{"cli/main.go"})
	if len(filtered) != 0 {
		t.Errorf("expected web/**/*.ts to not match cli/main.go")
	}
}

func TestRunChecks_AllPass(t *testing.T) {
	checks := []CheckConfig{
		{Name: "echo", Command: "echo hello"},
		{Name: "true", Command: "true"},
	}

	results, err := RunChecks(context.Background(), checks, t.TempDir())
	if err != nil {
		t.Fatalf("RunChecks error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		if !r.Passed {
			t.Errorf("check %q should have passed", r.Name)
		}
	}
}

func TestRunChecks_FailFast(t *testing.T) {
	checks := []CheckConfig{
		{Name: "pass", Command: "true"},
		{Name: "fail", Command: "false"},
		{Name: "never", Command: "echo should-not-run"},
	}

	results, err := RunChecks(context.Background(), checks, t.TempDir())
	if err != nil {
		t.Fatalf("RunChecks error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results (fail-fast), got %d", len(results))
	}
	if !results[0].Passed {
		t.Error("first check should have passed")
	}
	if results[1].Passed {
		t.Error("second check should have failed")
	}
}

func TestRunChecks_CapturesOutput(t *testing.T) {
	checks := []CheckConfig{
		{Name: "output", Command: "echo 'error: something broke' >&2; exit 1"},
	}

	results, err := RunChecks(context.Background(), checks, t.TempDir())
	if err != nil {
		t.Fatalf("RunChecks error: %v", err)
	}
	if results[0].Passed {
		t.Error("should have failed")
	}
	if !strings.Contains(results[0].Output, "error: something broke") {
		t.Errorf("expected error output, got %q", results[0].Output)
	}
}

func TestRunChecks_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	checks := []CheckConfig{
		{Name: "slow", Command: "sleep 10"},
	}

	results, err := RunChecks(ctx, checks, t.TempDir())
	if err != nil {
		t.Fatalf("RunChecks error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Passed {
		t.Error("should have failed due to timeout")
	}
}

func TestFormatFailure(t *testing.T) {
	result := CheckResult{
		Name:   "typecheck",
		Passed: false,
		Output: "error TS2345: Argument of type 'string' is not assignable\n",
	}

	msg := FormatFailure(result, "pnpm --filter web exec tsc --noEmit")

	if !strings.Contains(msg, "Task incomplete: typecheck failed") {
		t.Error("expected failure header")
	}
	if !strings.Contains(msg, "pnpm --filter web exec tsc --noEmit") {
		t.Error("expected command")
	}
	if !strings.Contains(msg, "TS2345") {
		t.Error("expected error output")
	}
	if !strings.Contains(msg, "Fix the issues above") {
		t.Error("expected fix instruction")
	}
}

func TestRunChecks_DurationMs(t *testing.T) {
	checks := []CheckConfig{
		{Name: "slow", Command: "sleep 0.05 && echo done"},
	}

	results, err := RunChecks(context.Background(), checks, t.TempDir())
	if err != nil {
		t.Fatalf("RunChecks error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].DurationMs < 40 {
		t.Errorf("DurationMs = %d, expected >= 40ms for 50ms sleep", results[0].DurationMs)
	}
}

func TestFilterChecks_EmptyGlobs(t *testing.T) {
	// Checks with empty globs should still match when files changed
	checks := []CheckConfig{
		{Name: "test", Command: "true", Globs: []string{}},
	}
	// Empty globs means "no globs specified" = always runs
	filtered := FilterChecks(checks, []string{"anything.txt"})
	if len(filtered) != 1 {
		t.Errorf("check with empty globs should match, got %d", len(filtered))
	}
}

func TestTruncate(t *testing.T) {
	short := "hello"
	if truncate(short, 100) != short {
		t.Error("short strings should not be truncated")
	}

	long := strings.Repeat("line of output\n", 200)
	truncated := truncate(long, 200)
	if len(truncated) > 220 { // some slack for truncation message
		t.Errorf("expected truncated output, got len=%d", len(truncated))
	}
	if !strings.Contains(truncated, "[output truncated]") {
		t.Error("expected truncation marker")
	}
}
