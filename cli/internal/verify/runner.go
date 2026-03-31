package verify

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const maxOutputLen = 2000

// CheckResult captures the outcome of a single verification check.
type CheckResult struct {
	Name       string
	Passed     bool
	Output     string
	ExitCode   int
	DurationMs int64
}

// FilterChecks returns only the checks whose globs match at least one
// changed file. Checks with no globs always match when there are changed files.
func FilterChecks(checks []CheckConfig, changedFiles []string) []CheckConfig {
	if len(changedFiles) == 0 {
		return nil
	}

	var filtered []CheckConfig
	for _, c := range checks {
		if len(c.Globs) == 0 {
			// No globs = always relevant when files changed
			filtered = append(filtered, c)
			continue
		}

		for _, f := range changedFiles {
			if matchesAnyGlob(f, c.Globs) {
				filtered = append(filtered, c)
				break
			}
		}
	}
	return filtered
}

// RunChecks executes each check sequentially, stopping at the first failure.
// Commands run via sh -c with the given directory as working directory.
func RunChecks(ctx context.Context, checks []CheckConfig, dir string) ([]CheckResult, error) {
	var results []CheckResult

	for _, c := range checks {
		cmd := exec.CommandContext(ctx, "sh", "-c", c.Command)
		cmd.Dir = dir

		start := time.Now()
		out, err := cmd.CombinedOutput()
		elapsed := time.Since(start)
		output := truncate(string(out), maxOutputLen)

		exitCode := 0
		passed := true
		if err != nil {
			passed = false
			if exitErr, ok := err.(*exec.ExitError); ok {
				if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
					exitCode = status.ExitStatus()
				} else {
					exitCode = 1
				}
			} else {
				exitCode = 1
				output = err.Error()
			}
		}

		results = append(results, CheckResult{
			Name:       c.Name,
			Passed:     passed,
			Output:     output,
			ExitCode:   exitCode,
			DurationMs: elapsed.Milliseconds(),
		})

		if !passed {
			break // fail-fast
		}
	}

	return results, nil
}

// FormatFailure produces the stderr message for a failed verification check.
func FormatFailure(result CheckResult, command string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Task incomplete: %s failed.\n", result.Name)
	fmt.Fprintf(&sb, "Command: %s\n\n", command)
	if result.Output != "" {
		sb.WriteString(result.Output)
		if !strings.HasSuffix(result.Output, "\n") {
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\nFix the issues above before completing this task.\n")
	return sb.String()
}

// FindCheck returns the CheckConfig for a given result name.
func FindCheck(checks []CheckConfig, name string) *CheckConfig {
	for _, c := range checks {
		if c.Name == name {
			return &c
		}
	}
	return nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	// Find a line break near the end to avoid cutting mid-line
	cutpoint := max - 20
	if cutpoint < 0 {
		cutpoint = 0
	}
	lastNewline := strings.LastIndex(s[:cutpoint], "\n")
	if lastNewline > cutpoint/2 {
		return s[:lastNewline] + "\n[output truncated]\n"
	}
	return s[:max-20] + "\n[output truncated]\n"
}

// matchesAnyGlobForFilter checks if a file path matches any glob in the list.
// This is the same as matchesAnyGlob but exported for use in filter context.
// We reuse the unexported matchesAnyGlob from snapshot.go since they're in the same package.

// matchesFileGlob is a helper for FilterChecks that normalizes paths before matching.
func matchesFileGlob(filePath string, pattern string) bool {
	// Normalize separators
	filePath = filepath.ToSlash(filePath)
	pattern = filepath.ToSlash(pattern)

	return matchesAnyGlob(filePath, []string{pattern})
}
