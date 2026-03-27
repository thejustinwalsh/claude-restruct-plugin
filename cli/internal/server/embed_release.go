//go:build !debug

package server

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// WebDist returns the embedded web dist filesystem.
func WebDist() fs.FS {
	sub, _ := fs.Sub(distFS, "dist")
	return sub
}

// IsDevMode returns false in release builds.
func IsDevMode() bool { return false }
