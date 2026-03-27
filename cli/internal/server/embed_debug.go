//go:build debug

package server

import "io/fs"

// WebDist returns nil in debug builds — the server proxies to Vite dev server.
func WebDist() fs.FS { return nil }

// IsDevMode returns true in debug builds.
func IsDevMode() bool { return true }
