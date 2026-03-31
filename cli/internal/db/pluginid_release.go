//go:build !debug

package db

// pluginID for marketplace installs: "restruct @ thejustinwalsh" → "restruct-thejustinwalsh"
const pluginID = "restruct-thejustinwalsh"

// BuildMode indicates whether this is a debug or release build.
const BuildMode = "release"
