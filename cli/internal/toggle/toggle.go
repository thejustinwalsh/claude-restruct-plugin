// Package toggle manages the enabled/disabled state of restruct via a sentinel file.
package toggle

import (
	"os"
	"path/filepath"
)

const sentinelFile = "disabled"

// IsEnabled returns true if restruct is enabled (no sentinel file).
func IsEnabled(dataDir string) bool {
	_, err := os.Stat(filepath.Join(dataDir, sentinelFile))
	return os.IsNotExist(err)
}

// Disable creates the sentinel file, disabling restruct.
func Disable(dataDir string) error {
	os.MkdirAll(dataDir, 0755)
	return os.WriteFile(filepath.Join(dataDir, sentinelFile), []byte("disabled by restruct disable\n"), 0644)
}

// Enable removes the sentinel file, enabling restruct.
func Enable(dataDir string) error {
	err := os.Remove(filepath.Join(dataDir, sentinelFile))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
