package permit

import (
	"os"
	"path/filepath"
	"strings"
)

// Canonicalize resolves a path to its absolute, symlink-resolved form.
// Returns empty string on error (caller should treat as "outside project").
func Canonicalize(path string) string {
	if path == "" {
		return ""
	}

	// Expand ~ to home directory
	if strings.HasPrefix(path, "~/") || path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		path = filepath.Join(home, path[1:])
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}

	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// Path may not exist yet (Write tool creating new file).
		// Walk up to find the first existing ancestor.
		resolved = resolveClosestAncestor(abs)
	}
	return resolved
}

// IsInside checks if path is inside or equal to root.
// Both must be canonicalized first.
func IsInside(path, root string) bool {
	if path == "" || root == "" {
		return false
	}
	if path == root {
		return true
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
}

// IsInsideAny checks if path is inside any of the given roots.
func IsInsideAny(path string, roots []string) bool {
	for _, root := range roots {
		if IsInside(path, root) {
			return true
		}
	}
	return false
}

// ResolveAllowedPaths takes config-relative paths and the project root,
// canonicalizes them, and returns absolute resolved paths.
func ResolveAllowedPaths(projectDir string, paths []string) []string {
	var resolved []string
	for _, p := range paths {
		var abs string
		if filepath.IsAbs(p) {
			abs = p
		} else if strings.HasPrefix(p, "~/") || p == "~" {
			home, err := os.UserHomeDir()
			if err != nil {
				continue
			}
			abs = filepath.Join(home, p[2:])
		} else {
			abs = filepath.Join(projectDir, p)
		}
		c := Canonicalize(abs)
		if c != "" {
			resolved = append(resolved, c)
		}
	}
	return resolved
}

// resolveClosestAncestor walks up the path to find the first real directory,
// then appends the unresolved tail. Handles the case where a tool is creating
// a new file in an existing directory.
func resolveClosestAncestor(abs string) string {
	dir := filepath.Dir(abs)
	base := filepath.Base(abs)
	for dir != "/" && dir != "." {
		resolved, err := filepath.EvalSymlinks(dir)
		if err == nil {
			return filepath.Join(resolved, base)
		}
		base = filepath.Join(filepath.Base(dir), base)
		dir = filepath.Dir(dir)
	}
	return abs
}
