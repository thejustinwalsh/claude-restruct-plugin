package verify

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tjw/restruct/internal/db"
)

type snapEntry struct {
	modTime int64
	size    int64
}

// TakeSnapshot records the mtime and size of all files matching globs
// in projectDir. Replaces any existing snapshot for this session+scope.
// Uses a single transaction for speed.
func TakeSnapshot(d *db.DB, sessionID, scope, projectDir string, globs []string) error {
	pool := d.Pool()

	tx, err := pool.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Delete existing snapshot for this key
	if _, err := tx.Exec("DELETE FROM snapshots WHERE session_id = ? AND scope = ?", sessionID, scope); err != nil {
		return err
	}

	stmt, err := tx.Prepare("INSERT INTO snapshots (session_id, scope, file_path, mod_time, file_size) VALUES (?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	// Use git ls-files to respect .gitignore. Lists tracked files and
	// untracked-but-not-ignored files (--cached --others --exclude-standard).
	files, err := gitListFiles(projectDir)
	if err != nil {
		// Fallback: not a git repo or git not available — walk manually
		files, err = walkFiles(projectDir)
		if err != nil {
			return err
		}
	}

	for _, rel := range files {
		if !matchesAnyGlob(rel, globs) {
			continue
		}

		abs := filepath.Join(projectDir, rel)
		info, err := os.Stat(abs)
		if err != nil {
			continue // file may have been deleted between listing and stat
		}

		if _, err := stmt.Exec(sessionID, scope, rel, info.ModTime().UnixNano(), info.Size()); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// DiffSnapshot compares stored snapshot against current file state.
// Returns relative paths of files that were added, removed, or modified.
func DiffSnapshot(d *db.DB, sessionID, scope, projectDir string) ([]string, error) {
	pool := d.Pool()

	rows, err := pool.Query(
		"SELECT file_path, mod_time, file_size FROM snapshots WHERE session_id = ? AND scope = ?",
		sessionID, scope,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	snapped := make(map[string]snapEntry)
	for rows.Next() {
		var path string
		var e snapEntry
		if err := rows.Scan(&path, &e.modTime, &e.size); err != nil {
			return nil, err
		}
		snapped[path] = e
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// If no snapshot exists, return nil (no baseline = no diff)
	if len(snapped) == 0 {
		return nil, nil
	}

	var changed []string
	seen := make(map[string]bool)

	// Check each snapshotted file against current state
	for path, prev := range snapped {
		seen[path] = true
		abs := filepath.Join(projectDir, path)
		info, err := os.Stat(abs)
		if err != nil {
			// File was deleted
			changed = append(changed, path)
			continue
		}
		if info.ModTime().UnixNano() != prev.modTime || info.Size() != prev.size {
			changed = append(changed, path)
		}
	}

	// Check for new files: walk the project dir using the same globs
	// that were used for the snapshot. We derive globs from the snapshot
	// extensions to avoid needing the config here.
	// Actually, we need to detect new files too. Walk and check if not in snapshot.
	// To keep this fast, we only check directories that contain snapshotted files.
	dirs := collectDirs(snapped)

	for dir := range dirs {
		abs := filepath.Join(projectDir, dir)
		entries, err := os.ReadDir(abs)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			rel := filepath.Join(dir, e.Name())
			if !seen[rel] {
				// New file in a tracked directory — check if it matches
				// common source extensions
				if isSourceFile(e.Name()) {
					changed = append(changed, rel)
				}
			}
		}
	}

	return changed, nil
}

// CleanSnapshot removes snapshot entries for session+scope.
func CleanSnapshot(d *db.DB, sessionID, scope string) error {
	_, err := d.Pool().Exec("DELETE FROM snapshots WHERE session_id = ? AND scope = ?", sessionID, scope)
	return err
}

// PruneStaleSnapshots deletes entries older than maxAge.
func PruneStaleSnapshots(d *db.DB, maxAge time.Duration) (int64, error) {
	cutoff := time.Now().Add(-maxAge).UTC().Format("2006-01-02 15:04:05")
	result, err := d.Pool().Exec("DELETE FROM snapshots WHERE created_at < ?", cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// CollectGlobs returns the union of all globs from all checks.
// Checks with no globs are tracked via a broad "**/*" pattern to
// ensure we detect any file change that could affect them.
func CollectGlobs(cfg *VerifyConfig) []string {
	seen := make(map[string]bool)
	hasUnglobbed := false

	for _, c := range cfg.Checks {
		if len(c.Globs) == 0 {
			hasUnglobbed = true
			continue
		}
		for _, g := range c.Globs {
			seen[g] = true
		}
	}

	if hasUnglobbed {
		// If any check has no globs, we need to track all source files
		seen["**/*"] = true
	}

	globs := make([]string, 0, len(seen))
	for g := range seen {
		globs = append(globs, g)
	}
	return globs
}

// HasSnapshot returns true if a snapshot exists for this session+scope.
func HasSnapshot(d *db.DB, sessionID, scope string) (bool, error) {
	var count int
	err := d.Pool().QueryRow(
		"SELECT COUNT(*) FROM snapshots WHERE session_id = ? AND scope = ? LIMIT 1",
		sessionID, scope,
	).Scan(&count)
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	return count > 0, nil
}

// matchesAnyGlob checks if a relative path matches any of the given glob patterns.
func matchesAnyGlob(rel string, globs []string) bool {
	if len(globs) == 0 {
		return false
	}

	for _, pattern := range globs {
		if pattern == "**/*" {
			// Match all source files
			if isSourceFile(filepath.Base(rel)) {
				return true
			}
			continue
		}

		// Handle ** prefix: match any directory depth
		if strings.HasPrefix(pattern, "**/") {
			suffix := pattern[3:]
			// Match against just the filename or any subpath
			matched, _ := filepath.Match(suffix, filepath.Base(rel))
			if matched {
				return true
			}
			// Also try matching the full relative path with each possible prefix
			parts := strings.Split(rel, string(filepath.Separator))
			for i := range parts {
				sub := strings.Join(parts[i:], string(filepath.Separator))
				matched, _ = filepath.Match(suffix, sub)
				if matched {
					return true
				}
			}
			continue
		}

		// Handle dir-prefixed globs like "web/**/*.ts"
		if strings.Contains(pattern, "/**/") {
			idx := strings.Index(pattern, "/**/")
			dirPrefix := pattern[:idx]
			suffix := pattern[idx+4:]

			if !strings.HasPrefix(rel, dirPrefix+string(filepath.Separator)) {
				continue
			}
			rest := rel[len(dirPrefix)+1:]
			matched, _ := filepath.Match(suffix, filepath.Base(rest))
			if matched {
				return true
			}
			continue
		}

		// Simple glob match
		matched, _ := filepath.Match(pattern, rel)
		if matched {
			return true
		}
	}
	return false
}

// collectDirs extracts the set of directory paths from snapshot file paths.
func collectDirs(files map[string]snapEntry) map[string]bool {
	dirs := make(map[string]bool)
	for path := range files {
		dir := filepath.Dir(path)
		dirs[dir] = true
	}
	return dirs
}

// gitListFiles returns relative paths of all files git knows about:
// tracked files (--cached) and untracked non-ignored files (--others --exclude-standard).
// Returns error if not in a git repo or git is not available.
func gitListFiles(dir string) ([]string, error) {
	cmd := exec.Command("git", "ls-files", "--cached", "--others", "--exclude-standard")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var files []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

// walkFiles is a fallback for non-git directories (e.g., tests).
// Walks the directory tree, skipping hidden dirs and common non-source dirs.
func walkFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "dist" || name == "build" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return nil
		}
		files = append(files, rel)
		return nil
	})
	return files, err
}

// isSourceFile returns true for common source file extensions.
func isSourceFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".rs", ".java",
		".c", ".cpp", ".h", ".hpp", ".cs", ".rb", ".swift", ".kt",
		".scala", ".lua", ".zig", ".vue", ".svelte", ".css", ".scss",
		".html", ".json", ".yaml", ".yml", ".toml", ".sql", ".sh",
		".md", ".txt", ".xml", ".proto", ".graphql":
		return true
	}
	return false
}
