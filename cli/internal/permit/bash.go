package permit

import (
	"strings"
)

// BashToken represents a single command segment in a bash command string.
type BashToken struct {
	Command string   // base command name (e.g., "cat", "curl", "git")
	Args    []string // remaining arguments after the command
	Raw     string   // original segment text
}

// TokenizeBash splits a bash command string into logical command segments.
// Splits on pipes (|), semicolons (;), &&, ||.
// This is intentionally conservative: if we can't parse it cleanly,
// the segment is marked with an empty Command.
func TokenizeBash(command string) []BashToken {
	segments := splitOnOperators(command)
	var tokens []BashToken
	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		tokens = append(tokens, parseSegment(seg))
	}
	return tokens
}

// HasRedirection checks if the command contains output redirection (>, >>).
func HasRedirection(command string) bool {
	for i := 0; i < len(command); i++ {
		if command[i] == '>' {
			return true
		}
	}
	return false
}

// HasSubshell checks for $(...) or backtick subshells.
func HasSubshell(command string) bool {
	return strings.Contains(command, "$(") || strings.Contains(command, "`")
}

// HasEnvExpansion checks for environment variable references that could
// resolve to arbitrary values.
func HasEnvExpansion(command string) bool {
	for i := 0; i < len(command)-1; i++ {
		if command[i] == '$' {
			next := command[i+1]
			// $VAR or ${VAR} but not $? $! $# $$ $0-$9
			if next == '{' || (next >= 'A' && next <= 'Z') || (next >= 'a' && next <= 'z') || next == '_' {
				return true
			}
		}
	}
	return false
}

// splitOnOperators splits on |, ;, &&, ||.
// Does NOT handle quotes — intentionally conservative.
// Over-splitting in quoted contexts means we classify more things
// as ambiguous, never fewer.
func splitOnOperators(s string) []string {
	var segments []string
	var current strings.Builder
	i := 0
	for i < len(s) {
		switch {
		case i+1 < len(s) && s[i] == '|' && s[i+1] == '|':
			segments = append(segments, current.String())
			current.Reset()
			i += 2
		case i+1 < len(s) && s[i] == '&' && s[i+1] == '&':
			segments = append(segments, current.String())
			current.Reset()
			i += 2
		case s[i] == '|' || s[i] == ';':
			segments = append(segments, current.String())
			current.Reset()
			i++
		default:
			current.WriteByte(s[i])
			i++
		}
	}
	if current.Len() > 0 {
		segments = append(segments, current.String())
	}
	return segments
}

// parseSegment extracts the command name and args from a single command segment.
func parseSegment(seg string) BashToken {
	fields := strings.Fields(seg)
	if len(fields) == 0 {
		return BashToken{Raw: seg}
	}

	// Skip leading env var assignments (FOO=bar cmd ...)
	cmdIdx := 0
	for cmdIdx < len(fields) {
		f := fields[cmdIdx]
		if strings.Contains(f, "=") && !strings.HasPrefix(f, "-") && !strings.HasPrefix(f, "/") {
			cmdIdx++
			continue
		}
		break
	}

	if cmdIdx >= len(fields) {
		return BashToken{Raw: seg}
	}

	cmd := fields[cmdIdx]
	args := fields[cmdIdx+1:]

	return BashToken{
		Command: cmd,
		Args:    args,
		Raw:     seg,
	}
}
