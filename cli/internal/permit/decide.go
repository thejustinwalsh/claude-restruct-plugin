package permit

import (
	"fmt"
	"strings"
)

// Decision is the final permission verdict.
type Decision struct {
	Action string // "allow", "deny", "" (passthrough)
	Reason string
	Tier   int // 1-8 security tier
}

// Decide orchestrates classification and returns a permission decision.
// This is the single entry point called by the permit command.
func Decide(toolName string, toolInput map[string]any, permissionMode string, projectDir string, cfg *Config) Decision {
	// Check always_ask rules first (highest priority override)
	if matchesAlwaysAsk(toolName, toolInput, cfg.AlwaysAsk) {
		return Decision{Action: "", Reason: "Matches always_ask rule", Tier: 8}
	}

	tc := ClassifyTool(toolName, toolInput)
	canonicalProject := Canonicalize(projectDir)
	allowedPaths := ResolveAllowedPaths(projectDir, cfg.AllowedPaths)

	switch tc.Category {
	case CategoryAlwaysAllow:
		return Decision{Action: "allow", Reason: tc.Description, Tier: 1}

	case CategoryReadOnly:
		return decidePathBased(tc.Paths, canonicalProject, allowedPaths, true, cfg)

	case CategoryWrite:
		if !cfg.AutoApproveWrites {
			return Decision{Action: "", Reason: "auto_approve_writes is false", Tier: 7}
		}
		return decidePathBased(tc.Paths, canonicalProject, allowedPaths, false, cfg)

	case CategoryNetwork:
		return decideNetwork(tc.URLs, cfg)

	case CategoryBash:
		return decideBash(toolInput, canonicalProject, allowedPaths, permissionMode, cfg)

	default:
		return Decision{Action: "", Reason: tc.Description, Tier: 8}
	}
}

// decidePathBased checks if all paths are inside project root or allowed paths.
func decidePathBased(paths []string, canonicalProject string, allowedPaths []string, readOnly bool, cfg *Config) Decision {
	tier := 1
	if !readOnly {
		tier = 2
	}

	for _, p := range paths {
		canon := Canonicalize(p)
		if IsInside(canon, canonicalProject) {
			continue
		}
		if IsInsideAny(canon, allowedPaths) {
			if readOnly {
				tier = 3
			} else {
				tier = 4
			}
			continue
		}
		desc := "Read"
		if !readOnly {
			desc = "Write"
		}
		return Decision{
			Action: "",
			Reason: fmt.Sprintf("%s outside allowed paths: %s", desc, p),
			Tier:   7,
		}
	}

	desc := "Read-only"
	if !readOnly {
		desc = "Write"
	}
	return Decision{Action: "allow", Reason: fmt.Sprintf("%s inside allowed paths", desc), Tier: tier}
}

// decideNetwork checks URLs against trusted/blocked lists.
func decideNetwork(urls []string, cfg *Config) Decision {
	for _, url := range urls {
		if matchesAnyURLPattern(url, cfg.BlockedURLs) {
			return Decision{Action: "deny", Reason: fmt.Sprintf("Blocked URL: %s", url), Tier: 6}
		}
	}

	allTrusted := true
	for _, url := range urls {
		if !matchesAnyURLPattern(url, cfg.TrustedURLs) {
			allTrusted = false
			break
		}
	}
	if allTrusted && len(urls) > 0 {
		return Decision{Action: "allow", Reason: "Trusted URL", Tier: 5}
	}

	return Decision{Action: "", Reason: "Untrusted network request", Tier: 7}
}

// decideBash handles the complex case of Bash tool classification.
func decideBash(toolInput map[string]any, canonicalProject string, allowedPaths []string, permissionMode string, cfg *Config) Decision {
	command, _ := toolInput["command"].(string)
	if command == "" {
		return Decision{Action: "", Reason: "Empty bash command", Tier: 8}
	}

	tokens := TokenizeBash(command)
	bc := ClassifyBash(tokens, command)

	// Destructive commands — always deny
	if bc.IsDestructive {
		return Decision{Action: "deny", Reason: bc.Description, Tier: 6}
	}

	// Network exfiltration check (highest priority for network commands)
	if bc.IsNetwork {
		risk, reason := DetectExfiltration(command, bc.URLs, cfg)
		if risk == ExfilHigh {
			return Decision{Action: "deny", Reason: reason, Tier: 6}
		}
	}

	// Unclassifiable — passthrough
	if bc.Unclassifiable {
		return Decision{Action: "", Reason: "Bash command could not be fully classified", Tier: 8}
	}

	// Pure read-only bash
	if bc.IsReadOnly {
		return decideBashReadOnly(bc, canonicalProject, allowedPaths)
	}

	// Network bash (not exfil, not destructive)
	if bc.IsNetwork && !bc.IsWrite {
		return decideBashNetwork(bc, cfg)
	}

	// Write bash (may also be network, e.g., git push)
	if bc.IsWrite {
		if !cfg.AutoApproveWrites {
			return Decision{Action: "", Reason: "auto_approve_writes is false", Tier: 7}
		}
		return decideBashWrite(bc, canonicalProject, allowedPaths)
	}

	return Decision{Action: "", Reason: "Ambiguous bash command", Tier: 8}
}

func decideBashReadOnly(bc BashClassification, canonicalProject string, allowedPaths []string) Decision {
	if len(bc.Paths) == 0 {
		return Decision{Action: "allow", Reason: "Read-only bash command", Tier: 1}
	}

	tier := 1
	for _, p := range bc.Paths {
		canon := Canonicalize(p)
		if IsInside(canon, canonicalProject) {
			continue
		}
		if IsInsideAny(canon, allowedPaths) {
			tier = 3
			continue
		}
		return Decision{Action: "", Reason: fmt.Sprintf("Bash read outside allowed paths: %s", p), Tier: 7}
	}
	return Decision{Action: "allow", Reason: "Read-only bash inside allowed paths", Tier: tier}
}

func decideBashWrite(bc BashClassification, canonicalProject string, allowedPaths []string) Decision {
	if len(bc.Paths) == 0 {
		// Write command with no detectable paths — allow inside project context
		return Decision{Action: "allow", Reason: "Bash write (no explicit paths)", Tier: 2}
	}

	tier := 2
	for _, p := range bc.Paths {
		canon := Canonicalize(p)
		if IsInside(canon, canonicalProject) {
			continue
		}
		if IsInsideAny(canon, allowedPaths) {
			tier = 4
			continue
		}
		return Decision{Action: "", Reason: fmt.Sprintf("Bash write outside allowed paths: %s", p), Tier: 7}
	}
	return Decision{Action: "allow", Reason: "Bash write inside allowed paths", Tier: tier}
}

func decideBashNetwork(bc BashClassification, cfg *Config) Decision {
	for _, url := range bc.URLs {
		if matchesAnyURLPattern(url, cfg.BlockedURLs) {
			return Decision{Action: "deny", Reason: fmt.Sprintf("Blocked URL: %s", url), Tier: 6}
		}
	}

	allTrusted := true
	for _, url := range bc.URLs {
		if !matchesAnyURLPattern(url, cfg.TrustedURLs) {
			allTrusted = false
			break
		}
	}
	if allTrusted && len(bc.URLs) > 0 {
		return Decision{Action: "allow", Reason: "Bash network to trusted URLs", Tier: 5}
	}

	if len(bc.URLs) == 0 {
		return Decision{Action: "", Reason: "Network command without parsed URL", Tier: 7}
	}

	return Decision{Action: "", Reason: "Bash network to untrusted URL", Tier: 7}
}

// matchesAlwaysAsk checks if a tool call matches any always_ask pattern.
// Pattern format: "ToolName(glob)" e.g., "Bash(rm -rf *)", "Bash(git push *)"
func matchesAlwaysAsk(toolName string, toolInput map[string]any, patterns []string) bool {
	for _, p := range patterns {
		prefix := toolName + "("
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		inner := strings.TrimPrefix(p, prefix)
		inner = strings.TrimSuffix(inner, ")")

		// For Bash, match against the command
		if toolName == "Bash" {
			cmd, _ := toolInput["command"].(string)
			if cmd != "" && matchGlobLike(cmd, inner) {
				return true
			}
		}

		// For file tools, match against the path
		if toolName == "Write" || toolName == "Edit" || toolName == "Read" {
			path, _ := toolInput["file_path"].(string)
			if path != "" && matchGlobLike(path, inner) {
				return true
			}
		}
	}
	return false
}

// matchGlobLike does simple glob matching where * matches any characters.
func matchGlobLike(s, pattern string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return s == pattern
	}
	pos := 0
	for i, part := range parts {
		if part == "" {
			continue
		}
		idx := strings.Index(s[pos:], part)
		if idx < 0 {
			return false
		}
		if i == 0 && idx != 0 {
			return false
		}
		pos += idx + len(part)
	}
	return true
}
