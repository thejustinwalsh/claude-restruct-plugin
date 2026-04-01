package permit

import (
	"os"
	"strings"
)

// BashClassification is the result of analyzing a bash command.
type BashClassification struct {
	IsReadOnly     bool
	IsWrite        bool
	IsNetwork      bool
	IsDestructive  bool
	URLs           []string
	Paths          []string
	HasRedirection bool
	HasSubshell    bool
	Unclassifiable bool
	Description    string
}

// readOnlyCommands are bash commands that never modify state.
var readOnlyCommands = map[string]bool{
	"cat": true, "head": true, "tail": true, "less": true, "more": true,
	"ls": true, "find": true, "grep": true, "rg": true, "ag": true, "ack": true,
	"wc": true, "sort": true, "uniq": true, "diff": true, "file": true,
	"stat": true, "which": true, "where": true, "type": true, "command": true,
	"echo": true, "printf": true, "env": true, "printenv": true,
	"pwd": true, "whoami": true, "hostname": true, "uname": true,
	"date": true, "cal": true, "uptime": true, "id": true,
	"tree": true, "du": true, "df": true,
	"jq": true, "yq": true, "xmllint": true,
	"xargs": true, "tr": true, "cut": true, "paste": true,
	"test": true, "[": true, "[[": true, "true": true, "false": true,
}

// readOnlyWithFlags are commands that are read-only UNLESS specific flags are present.
var readOnlyWithFlags = map[string][]string{
	"sed": {"-i"},
	"awk": {}, // awk is always read-only (output goes to stdout)
}

// gitReadSubcommands are git subcommands that don't modify state.
var gitReadSubcommands = map[string]bool{
	"log": true, "show": true, "diff": true, "status": true,
	"branch": true, "tag": true, "remote": true, "rev-parse": true,
	"ls-files": true, "blame": true, "shortlog": true, "describe": true,
	"config": true, "ls-remote": true, "cat-file": true, "reflog": true,
}

// goReadSubcommands are go subcommands that don't modify state.
var goReadSubcommands = map[string]bool{
	"test": true, "vet": true, "list": true, "version": true, "env": true,
	"doc": true, "fmt": true, "tool": true,
}

// writeCommands are commands that modify files or system state.
var writeCommands = map[string]bool{
	"rm": true, "rmdir": true, "mv": true, "cp": true,
	"mkdir": true, "touch": true, "chmod": true, "chown": true,
	"ln": true, "install": true, "patch": true,
	"tar": true, "unzip": true, "gzip": true, "gunzip": true,
}

// networkCommands are commands that make network requests.
var networkCommands = map[string]bool{
	"curl": true, "wget": true, "http": true, "fetch": true,
	"ssh": true, "scp": true, "rsync": true, "sftp": true,
	"ftp": true, "nc": true, "ncat": true, "telnet": true,
	"nslookup": true, "dig": true, "ping": true, "traceroute": true,
}

// neverAutoApprove are commands that should never be auto-approved.
var neverAutoApprove = map[string]bool{
	"eval": true, "exec": true, "source": true,
}

// packageManagers are commands that modify dependencies (write, project-scoped).
var packageManagers = map[string]bool{
	"npm": true, "pnpm": true, "yarn": true, "bun": true,
	"pip": true, "pip3": true, "poetry": true, "uv": true,
	"cargo": true, "go": true,
	"make": true, "xmake": true, "cmake": true,
	"tsc": true, "eslint": true, "prettier": true, "gofmt": true, "rustfmt": true,
}

// destructiveSubstrings are patterns checked via simple substring match.
// These don't involve path arguments that could false-positive.
var destructiveSubstrings = []string{
	"> /dev/",
	"dd if=",
	"mkfs.",
}

// destructiveRoots are canonicalized paths that rm should never target.
// Checked against resolved paths, not raw command strings, so
// traversal tricks (../../, symlinks) can't bypass them.
var destructiveRoots = []string{
	"/",
	"/bin", "/sbin", "/usr", "/etc", "/var", "/System", "/Library",
	"/boot", "/dev", "/proc", "/sys",
}

// ClassifyBash analyzes tokenized bash command segments.
func ClassifyBash(tokens []BashToken, fullCommand string) BashClassification {
	result := BashClassification{
		HasRedirection: HasRedirection(fullCommand),
		HasSubshell:    HasSubshell(fullCommand),
	}

	if len(tokens) == 0 {
		result.Unclassifiable = true
		result.Description = "Empty command"
		return result
	}

	// Check for destructive substring patterns (non-path)
	for _, pattern := range destructiveSubstrings {
		if strings.Contains(fullCommand, pattern) {
			result.IsDestructive = true
			result.Description = "Destructive command pattern: " + pattern
			return result
		}
	}

	// Check rm commands against destructive roots using canonicalized paths.
	// This prevents false positives (rm -rf /Users/x/foo ≠ rm -rf /)
	// while catching traversal tricks (rm -rf /tmp/../../etc).
	if reason := checkDestructiveRm(tokens); reason != "" {
		result.IsDestructive = true
		result.Description = reason
		return result
	}

	allReadOnly := true

	for _, tok := range tokens {
		if tok.Command == "" {
			result.Unclassifiable = true
			allReadOnly = false
			continue
		}

		cmd := tok.Command

		// Strip path prefix (e.g., /usr/bin/cat → cat)
		if strings.Contains(cmd, "/") {
			parts := strings.Split(cmd, "/")
			cmd = parts[len(parts)-1]
		}

		// Never auto-approve
		if neverAutoApprove[cmd] {
			result.Unclassifiable = true
			allReadOnly = false
			continue
		}

		// Network commands
		if networkCommands[cmd] {
			result.IsNetwork = true
			allReadOnly = false
			for _, arg := range tok.Args {
				if isURL(arg) {
					result.URLs = append(result.URLs, arg)
				}
			}
			continue
		}

		// Write commands
		if writeCommands[cmd] {
			result.IsWrite = true
			allReadOnly = false
			result.Paths = append(result.Paths, extractPathArgs(tok.Args)...)
			continue
		}

		// Git — classify by subcommand
		if cmd == "git" {
			subcmd := firstNonFlagArg(tok.Args)
			if subcmd == "push" || subcmd == "publish" {
				// git push is network + write
				result.IsNetwork = true
				result.IsWrite = true
				allReadOnly = false
			} else if subcmd == "clone" || subcmd == "pull" || subcmd == "fetch" {
				result.IsNetwork = true
				allReadOnly = false
			} else if gitReadSubcommands[subcmd] {
				// read-only git
			} else {
				// write git (commit, add, reset, etc.)
				result.IsWrite = true
				allReadOnly = false
			}
			continue
		}

		// Go — classify by subcommand
		if cmd == "go" {
			subcmd := firstNonFlagArg(tok.Args)
			if goReadSubcommands[subcmd] {
				// read-only go
			} else {
				// go build, go install, go mod tidy, go get, go generate
				result.IsWrite = true
				allReadOnly = false
			}
			continue
		}

		// Package managers — treat as writes (they modify node_modules, etc.)
		if packageManagers[cmd] {
			result.IsWrite = true
			allReadOnly = false
			continue
		}

		// Read-only commands
		if readOnlyCommands[cmd] {
			continue
		}

		// Read-only with flag check (e.g., sed without -i)
		if flags, ok := readOnlyWithFlags[cmd]; ok {
			hasWriteFlag := false
			for _, f := range flags {
				if containsFlag(tok.Args, f) {
					hasWriteFlag = true
					break
				}
			}
			if hasWriteFlag {
				result.IsWrite = true
				allReadOnly = false
			}
			continue
		}

		// Node/Python one-liners — treat as unclassifiable (could do anything)
		if cmd == "node" || cmd == "python" || cmd == "python3" || cmd == "ruby" {
			result.Unclassifiable = true
			allReadOnly = false
			continue
		}

		// Unknown command
		result.Unclassifiable = true
		allReadOnly = false
	}

	// Redirection makes any command a potential write
	if result.HasRedirection && !result.IsNetwork {
		allReadOnly = false
		result.IsWrite = true
	}

	// Subshells make classification unreliable
	if result.HasSubshell {
		result.Unclassifiable = true
		allReadOnly = false
	}

	result.IsReadOnly = allReadOnly && !result.Unclassifiable

	return result
}

func firstNonFlagArg(args []string) string {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}

func containsFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag || strings.HasPrefix(a, flag+"=") {
			return true
		}
	}
	return false
}

func isURL(s string) bool {
	return strings.HasPrefix(s, "http://") ||
		strings.HasPrefix(s, "https://") ||
		strings.HasPrefix(s, "ftp://")
}

// checkDestructiveRm checks if any rm/rmdir token targets a destructive root
// after full path canonicalization (symlink resolution, traversal normalization).
// expandHomePlaceholders replaces $HOME and ${HOME} with the actual home dir
// so the canonicalizer can resolve them. Shell doesn't run before us.
func expandHomePlaceholders(arg string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return arg
	}
	arg = strings.ReplaceAll(arg, "${HOME}", home)
	arg = strings.ReplaceAll(arg, "$HOME", home)
	return arg
}

// checkDestructiveRm checks if any rm/rmdir token targets a destructive root
// after full path canonicalization (symlink resolution, traversal normalization).
func checkDestructiveRm(tokens []BashToken) string {
	home, _ := os.UserHomeDir()
	for _, tok := range tokens {
		cmd := tok.Command
		if strings.Contains(cmd, "/") {
			parts := strings.Split(cmd, "/")
			cmd = parts[len(parts)-1]
		}
		if cmd != "rm" && cmd != "rmdir" {
			continue
		}
		for _, arg := range tok.Args {
			if strings.HasPrefix(arg, "-") {
				continue
			}
			expanded := expandHomePlaceholders(arg)
			resolved := Canonicalize(expanded)
			if resolved == "" {
				continue
			}
			// Check home directory
			if home != "" && resolved == home {
				return "Destructive target: home directory"
			}
			// Check system roots — match against both the resolved path
			// and the canonical form of each root (handles macOS symlinks
			// like /etc → /private/etc).
			for _, root := range destructiveRoots {
				canonRoot := Canonicalize(root)
				if resolved == root || resolved == canonRoot {
					return "Destructive target: " + root
				}
			}
		}
	}
	return ""
}

func extractPathArgs(args []string) []string {
	var paths []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") && !isURL(a) && len(a) > 0 {
			paths = append(paths, a)
		}
	}
	return paths
}
