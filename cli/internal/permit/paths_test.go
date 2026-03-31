package permit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCanonicalize(t *testing.T) {
	t.Run("empty path", func(t *testing.T) {
		if got := Canonicalize(""); got != "" {
			t.Errorf("Canonicalize(\"\") = %q, want empty", got)
		}
	})

	t.Run("absolute path", func(t *testing.T) {
		dir := t.TempDir()
		got := Canonicalize(dir)
		if got == "" {
			t.Error("Canonicalize should resolve existing dir")
		}
	})

	t.Run("nonexistent file in existing dir", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "newfile.txt")
		got := Canonicalize(path)
		if got == "" {
			t.Error("Canonicalize should resolve path with existing parent")
		}
		// Compare canonicalized forms (macOS /var → /private/var)
		canonDir := Canonicalize(dir)
		if filepath.Dir(got) != canonDir {
			t.Errorf("parent should be %s, got %s", canonDir, filepath.Dir(got))
		}
	})
}

func TestIsInside(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		root   string
		expect bool
	}{
		{"exact match", "/project", "/project", true},
		{"inside", "/project/src/file.go", "/project", true},
		{"outside", "/other/file.go", "/project", false},
		{"prefix but not child", "/project2/file.go", "/project", false},
		{"empty path", "", "/project", false},
		{"empty root", "/project/file.go", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsInside(tt.path, tt.root); got != tt.expect {
				t.Errorf("IsInside(%q, %q) = %v, want %v", tt.path, tt.root, got, tt.expect)
			}
		})
	}
}

func TestIsInsideAny(t *testing.T) {
	roots := []string{"/project", "/opt/data"}

	if !IsInsideAny("/project/src/main.go", roots) {
		t.Error("should be inside /project")
	}
	if !IsInsideAny("/opt/data/fixtures/test.json", roots) {
		t.Error("should be inside /opt/data")
	}
	if IsInsideAny("/other/path", roots) {
		t.Error("should not be inside any root")
	}
}

func TestResolveAllowedPaths(t *testing.T) {
	dir := t.TempDir()
	siblingDir := t.TempDir()

	// Create a relative path that resolves
	relPath, _ := filepath.Rel(dir, siblingDir)

	paths := ResolveAllowedPaths(dir, []string{relPath, "/nonexistent/should/still/resolve"})
	if len(paths) == 0 {
		t.Error("should resolve at least the sibling path")
	}
}

func TestSymlinkEscape(t *testing.T) {
	// Create: project/link -> /tmp (outside project)
	projectDir := t.TempDir()
	targetDir := t.TempDir()

	linkPath := filepath.Join(projectDir, "link")
	if err := os.Symlink(targetDir, linkPath); err != nil {
		t.Skip("symlinks not supported")
	}

	// A file "inside" the symlink should resolve outside the project
	filePath := filepath.Join(linkPath, "secret.txt")
	canon := Canonicalize(filePath)
	projectCanon := Canonicalize(projectDir)

	if IsInside(canon, projectCanon) {
		t.Error("symlink escape: file through symlink should resolve outside project root")
	}
}
