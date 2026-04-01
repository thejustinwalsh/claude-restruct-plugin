package bootstrap

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// MaxFiles is the default cap on discovered files to prevent timeout in large repos.
const MaxFiles = 50

// DiscoveredFile represents a single rule file found during discovery.
type DiscoveredFile struct {
	AbsPath string    // absolute filesystem path
	RelPath string    // relative to project root
	Depth   int       // directory depth from project root (0 = root level)
	Size    int64     // file size in bytes
	ModTime time.Time // last modification time
}

// DiscoverResult holds the output of a discovery scan.
type DiscoverResult struct {
	Files      []DiscoveredFile
	GitRoot    string
	ProjectDir string
	Duration   time.Duration
}

// Discover finds all rule files (CLAUDE.md, agents.md, etc.) under the project tree.
// Uses git ls-files for speed when available, falls back to filepath.WalkDir.
// Results are sorted by depth ascending (closest to root first) and capped at maxFiles.
func Discover(projectDir string, fileNames []string, maxFiles int) (*DiscoverResult, error) {
	start := time.Now()
	if maxFiles <= 0 {
		maxFiles = MaxFiles
	}

	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		return nil, err
	}

	gitRoot := findGitRoot(absProject)
	searchRoot := absProject
	if gitRoot != "" {
		searchRoot = gitRoot
	}

	var files []DiscoveredFile

	// Try git ls-files first (fast, respects .gitignore)
	if gitRoot != "" {
		files, err = discoverGit(searchRoot, absProject, fileNames)
		if err != nil {
			// Fallback to walk on git failure
			files = nil
		}
	}

	// Fallback: walk the filesystem
	if files == nil {
		files, err = discoverWalk(searchRoot, absProject, fileNames)
		if err != nil {
			return nil, err
		}
	}

	// Sort by depth ascending, then by path for stability
	sort.Slice(files, func(i, j int) bool {
		if files[i].Depth != files[j].Depth {
			return files[i].Depth < files[j].Depth
		}
		return files[i].RelPath < files[j].RelPath
	})

	// Cap at maxFiles
	if len(files) > maxFiles {
		files = files[:maxFiles]
	}

	return &DiscoverResult{
		Files:      files,
		GitRoot:    gitRoot,
		ProjectDir: absProject,
		Duration:   time.Since(start),
	}, nil
}

// discoverGit uses git ls-files to find rule files efficiently.
func discoverGit(gitRoot, projectDir string, fileNames []string) ([]DiscoveredFile, error) {
	// Build glob patterns for git ls-files
	var patterns []string
	for _, name := range fileNames {
		patterns = append(patterns, "**/" + name)
		patterns = append(patterns, name) // root level
	}

	args := []string{"ls-files", "--cached", "--others", "--exclude-standard"}
	for _, p := range patterns {
		args = append(args, p)
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = gitRoot
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var files []DiscoveredFile

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		absPath := filepath.Join(gitRoot, line)
		if seen[absPath] {
			continue
		}

		// Verify file exists and matches our target names
		base := filepath.Base(absPath)
		if !isTargetFile(base, fileNames) {
			continue
		}

		info, err := os.Stat(absPath)
		if err != nil {
			continue
		}

		relPath, err := filepath.Rel(projectDir, absPath)
		if err != nil {
			continue
		}

		seen[absPath] = true
		files = append(files, DiscoveredFile{
			AbsPath: absPath,
			RelPath: relPath,
			Depth:   pathDepth(relPath),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}

	return files, nil
}

// discoverWalk uses filepath.WalkDir as a fallback when git is unavailable.
func discoverWalk(root, projectDir string, fileNames []string) ([]DiscoveredFile, error) {
	targetSet := make(map[string]bool, len(fileNames))
	for _, name := range fileNames {
		targetSet[filepath.Base(name)] = true
	}

	var files []DiscoveredFile
	seen := make(map[string]bool)

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip inaccessible dirs
		}

		// Skip hidden directories and common non-project dirs
		if d.IsDir() {
			base := d.Name()
			if strings.HasPrefix(base, ".") && base != "." && base != ".claude" {
				return filepath.SkipDir
			}
			if base == "node_modules" || base == "vendor" || base == "dist" {
				return filepath.SkipDir
			}
			return nil
		}

		// For .claude/rules.md style paths, check if parent+name matches
		if !isTargetFile(filepath.Base(path), fileNames) {
			// Also check compound paths like ".claude/rules.md"
			relToRoot, err := filepath.Rel(root, path)
			if err != nil {
				return nil
			}
			found := false
			for _, name := range fileNames {
				if strings.Contains(name, "/") && strings.HasSuffix(relToRoot, name) {
					found = true
					break
				}
			}
			if !found {
				return nil
			}
		}

		abs, err := filepath.Abs(path)
		if err != nil {
			return nil
		}
		if seen[abs] {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		relPath, err := filepath.Rel(projectDir, abs)
		if err != nil {
			return nil
		}

		seen[abs] = true
		files = append(files, DiscoveredFile{
			AbsPath: abs,
			RelPath: relPath,
			Depth:   pathDepth(relPath),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})

		return nil
	})

	return files, err
}

// findGitRoot walks up from dir to find the .git directory.
func findGitRoot(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// pathDepth returns the number of directory separators in a relative path.
// "CLAUDE.md" → 0, "web/CLAUDE.md" → 1, "web/src/CLAUDE.md" → 2
func pathDepth(relPath string) int {
	if relPath == "." || relPath == "" {
		return 0
	}
	// Normalize to forward slashes for counting
	relPath = filepath.ToSlash(relPath)
	return strings.Count(relPath, "/")
}

// isTargetFile checks if a filename matches any of the target file names.
func isTargetFile(filename string, targets []string) bool {
	for _, t := range targets {
		// For simple names like "CLAUDE.md", match basename
		if !strings.Contains(t, "/") && filename == t {
			return true
		}
		// For compound paths like ".claude/rules.md", match the last component
		if strings.Contains(t, "/") && filename == filepath.Base(t) {
			return true
		}
	}
	return false
}
