package permit

import (
	"regexp"
	"strings"
)

// ExfilRisk indicates the level of data exfiltration risk.
type ExfilRisk int

const (
	ExfilNone ExfilRisk = iota
	ExfilHigh
)

// Compiled regexes for exfiltration pattern detection.
var (
	postDataFlags      = regexp.MustCompile(`\s(-d|--data|--data-raw|--data-binary|--data-urlencode|-F|--form)\s`)
	postMethodFlag     = regexp.MustCompile(`(-X\s*POST|-XPOST|--request\s*POST)`)
	dynamicURLPattern  = regexp.MustCompile(`(curl|wget)\s+[^|;]*(\$\{|\$\(|` + "`" + `)`)
	encodedExfilPipe   = regexp.MustCompile(`base64[^|]*\|[^|]*(curl|wget|nc)`)
	headerFlag         = regexp.MustCompile(`\s(-H|--header)\s+"?([^"]*)"?`)

	// Sensitive data patterns in content
	apiKeyPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)sk-[a-zA-Z0-9]{20,}`),                       // OpenAI-style
		regexp.MustCompile(`(?i)ghp_[a-zA-Z0-9]{36}`),                       // GitHub PAT
		regexp.MustCompile(`(?i)gho_[a-zA-Z0-9]{36}`),                       // GitHub OAuth
		regexp.MustCompile(`(?i)AKIA[0-9A-Z]{16}`),                          // AWS Access Key ID
		regexp.MustCompile(`(?i)xox[bpsa]-[a-zA-Z0-9\-]{10,}`),             // Slack tokens
		regexp.MustCompile(`(?i)-----BEGIN\s+(RSA|DSA|EC|OPENSSH)\s+PRIVATE`), // Private keys
	}

	// Default sensitive env var prefixes
	defaultSensitiveEnvPrefixes = []string{
		"API_KEY", "SECRET", "TOKEN", "PASSWORD", "PASSWD", "AUTH",
		"CREDENTIAL", "ACCESS_KEY", "PRIVATE_KEY",
		"AWS_", "GITHUB_TOKEN", "GH_TOKEN", "ANTHROPIC_API_KEY", "OPENAI_API_KEY",
		"DATABASE_URL", "REDIS_URL", "MONGO_URI",
		"NPM_TOKEN", "PYPI_TOKEN", "RUBYGEMS_API_KEY",
		"SSH_", "GPG_",
	}

	// Network target commands for pipe detection
	networkTargets = []string{"curl", "wget", "nc", "ncat", "ssh", "telnet"}

	// Sensitive source commands for pipe detection
	sensitiveSources = []string{"cat", "env", "printenv"}
)

// DetectExfiltration checks a bash command for data exfiltration patterns.
func DetectExfiltration(command string, urls []string, cfg *Config) (ExfilRisk, string) {
	// Check blocked URLs first
	for _, url := range urls {
		if matchesAnyURLPattern(url, cfg.BlockedURLs) {
			return ExfilHigh, "Blocked URL: " + url
		}
	}

	// If all URLs are trusted, skip deep inspection
	allTrusted := len(urls) > 0
	for _, url := range urls {
		if !matchesAnyURLPattern(url, cfg.TrustedURLs) {
			allTrusted = false
			break
		}
	}
	if allTrusted {
		return ExfilNone, ""
	}

	// Pattern 1: POST with data to untrusted URL
	if hasPostWithData(command) {
		return ExfilHigh, "POST with data to untrusted URL"
	}

	// Pattern 2: Piping sensitive content to network commands
	if reason := checkPipedExfil(command); reason != "" {
		return ExfilHigh, reason
	}

	// Pattern 3: Dynamic URL interpolation ($VAR in curl/wget URL)
	if dynamicURLPattern.MatchString(command) {
		return ExfilHigh, "Network request with dynamic/interpolated URL"
	}

	// Pattern 4: Base64 encode piped to network
	if encodedExfilPipe.MatchString(command) {
		return ExfilHigh, "Encoded data sent to network command"
	}

	// Pattern 5: Sensitive env var references in network context
	if reason := checkSensitiveEnvInNetwork(command, cfg.SensitiveEnvPatterns); reason != "" {
		return ExfilHigh, reason
	}

	// Pattern 6: API key patterns in command string
	if reason := checkAPIKeyPatterns(command); reason != "" {
		return ExfilHigh, reason
	}

	// Pattern 7: Header inspection for auth tokens
	if reason := checkSensitiveHeaders(command); reason != "" {
		return ExfilHigh, reason
	}

	return ExfilNone, ""
}

func hasPostWithData(cmd string) bool {
	return postDataFlags.MatchString(" "+cmd+" ") && postMethodFlag.MatchString(cmd)
}

func checkPipedExfil(cmd string) string {
	parts := strings.Split(cmd, "|")
	if len(parts) < 2 {
		return ""
	}

	left := strings.TrimSpace(parts[0])
	right := strings.TrimSpace(parts[len(parts)-1])

	hasSensitiveSource := false
	for _, src := range sensitiveSources {
		if strings.HasPrefix(left, src+" ") || left == src {
			hasSensitiveSource = true
			break
		}
	}
	// Also check for echo $SECRET patterns
	if strings.Contains(left, "echo $") || strings.Contains(left, "echo ${") {
		hasSensitiveSource = true
	}

	hasNetworkTarget := false
	for _, tgt := range networkTargets {
		if strings.HasPrefix(right, tgt+" ") || strings.HasPrefix(right, tgt+"\t") || right == tgt {
			hasNetworkTarget = true
			break
		}
	}

	if hasSensitiveSource && hasNetworkTarget {
		return "Piping sensitive content to network command"
	}
	return ""
}

func checkSensitiveEnvInNetwork(cmd string, extraPatterns []string) string {
	// Only relevant if there's a network command present
	hasNetwork := false
	for _, net := range networkTargets {
		if strings.Contains(cmd, net) {
			hasNetwork = true
			break
		}
	}
	if !hasNetwork {
		return ""
	}

	allPrefixes := append(defaultSensitiveEnvPrefixes, extraPatterns...)
	for _, prefix := range allPrefixes {
		prefix = strings.TrimSuffix(prefix, "*")
		// Check for $PREFIX or ${PREFIX in the command
		if strings.Contains(cmd, "$"+prefix) || strings.Contains(cmd, "${"+prefix) {
			return "Sensitive environment variable ($" + prefix + "...) in network request"
		}
	}
	return ""
}

func checkAPIKeyPatterns(cmd string) string {
	// Only check if there's a network command present
	hasNetwork := false
	for _, net := range networkTargets {
		if strings.Contains(cmd, net) {
			hasNetwork = true
			break
		}
	}
	if !hasNetwork {
		return ""
	}

	for _, re := range apiKeyPatterns {
		if re.MatchString(cmd) {
			return "API key or credential pattern detected in network command"
		}
	}
	return ""
}

func checkSensitiveHeaders(cmd string) string {
	if !strings.Contains(cmd, "curl") && !strings.Contains(cmd, "wget") {
		return ""
	}

	matches := headerFlag.FindAllStringSubmatch(cmd, -1)
	for _, m := range matches {
		if len(m) >= 3 {
			headerVal := m[2]
			lowerVal := strings.ToLower(headerVal)
			if strings.Contains(lowerVal, "authorization") ||
				strings.Contains(lowerVal, "x-api-key") ||
				strings.Contains(lowerVal, "x-auth-token") {
				// Check if the value references an env var (safe for our hooks)
				// or contains a literal token
				if strings.Contains(headerVal, "$") || strings.Contains(headerVal, "${") {
					return "Authorization header with environment variable in network request"
				}
			}
		}
	}
	return ""
}

// matchesAnyURLPattern checks if a URL matches any glob-like patterns.
// Supports * as wildcard.
func matchesAnyURLPattern(url string, patterns []string) bool {
	for _, p := range patterns {
		if matchURLPattern(url, p) {
			return true
		}
	}
	return false
}

func matchURLPattern(url, pattern string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return url == pattern
	}
	pos := 0
	for i, part := range parts {
		if part == "" {
			continue
		}
		idx := strings.Index(url[pos:], part)
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
