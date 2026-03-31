//go:build debug

package db

// pluginID for inline/dev installs — matches the directory Claude Code
// creates when using --plugin-dir (inline install).
const pluginID = "restruct-inline"

// BuildMode indicates whether this is a debug or release build.
const BuildMode = "debug"
