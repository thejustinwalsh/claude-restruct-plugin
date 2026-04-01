package bootstrap

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/tjw/restruct/internal/prompt"
)

// Document represents a processed rule file with extracted metadata.
type Document struct {
	Source     string            `json:"source"`      // relative path
	AbsPath   string            `json:"path"`        // absolute path
	Hash      string            `json:"hash"`        // first 8 chars of content SHA256
	Keywords  []string          `json:"keywords"`    // extracted terms
	Categories []string         `json:"categories"`  // context, constraints, workflow, anti-patterns
	Sections  int               `json:"sections"`    // number of ## sections
	RuleCount int               `json:"rule_count"`  // total bullet-point rules
	Summary   string            `json:"summary"`     // first meaningful line or header content
	Generated time.Time         `json:"generated"`
	Content   string            `json:"-"`           // raw file content (not serialized in index)
	Rules     *prompt.ParsedRules `json:"-"`         // parsed rules (not serialized in index)
}

// GenerateDocument reads a discovered file and produces a Document with
// structural metadata: keywords, categories, rule counts, and summary.
func GenerateDocument(file DiscoveredFile) (*Document, error) {
	data, err := os.ReadFile(file.AbsPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", file.AbsPath, err)
	}
	content := string(data)

	// Parse rules using the existing prompt.ParseRules
	rules := prompt.ParseRules(content)

	// Count sections (## headers)
	sectionCount := 0
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "## ") {
			sectionCount++
		}
	}

	// Determine categories present
	var categories []string
	if len(rules.ContextRules) > 0 {
		categories = append(categories, "context")
	}
	if len(rules.ConstraintRules) > 0 {
		categories = append(categories, "constraints")
	}
	if len(rules.WorkflowRules) > 0 {
		categories = append(categories, "workflow")
	}
	if len(rules.AntiPatterns) > 0 {
		categories = append(categories, "anti-patterns")
	}

	ruleCount := len(rules.ContextRules) + len(rules.ConstraintRules) +
		len(rules.WorkflowRules) + len(rules.AntiPatterns)

	// Extract keywords from all rules
	keywords := extractKeywords(content, rules)

	// Generate summary
	summary := extractSummary(content, file.RelPath)

	// Hash the content
	h := sha256.Sum256(data)
	hash := fmt.Sprintf("%x", h)[:8]

	return &Document{
		Source:     file.RelPath,
		AbsPath:    file.AbsPath,
		Hash:       hash,
		Keywords:   keywords,
		Categories: categories,
		Sections:   sectionCount,
		RuleCount:  ruleCount,
		Summary:    summary,
		Generated:  time.Now().UTC(),
		Content:    content,
		Rules:      rules,
	}, nil
}

// WriteDocument writes a deep-context document to .restruct/links/<hash>.md
// with YAML frontmatter.
func WriteDocument(doc *Document, linksDir string) error {
	if err := os.MkdirAll(linksDir, 0755); err != nil {
		return err
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("source: %s\n", doc.Source))
	b.WriteString(fmt.Sprintf("path: %s\n", doc.AbsPath))
	b.WriteString(fmt.Sprintf("hash: %s\n", doc.Hash))
	b.WriteString(fmt.Sprintf("keywords: [%s]\n", strings.Join(doc.Keywords, ", ")))
	b.WriteString(fmt.Sprintf("categories: [%s]\n", strings.Join(doc.Categories, ", ")))
	b.WriteString(fmt.Sprintf("sections: %d\n", doc.Sections))
	b.WriteString(fmt.Sprintf("rules: %d\n", doc.RuleCount))
	b.WriteString(fmt.Sprintf("summary: %s\n", doc.Summary))
	b.WriteString(fmt.Sprintf("generated: %s\n", doc.Generated.Format(time.RFC3339)))
	b.WriteString("---\n\n")

	// Write classified rules by category
	if len(doc.Rules.ContextRules) > 0 {
		b.WriteString("## Context Rules\n")
		for i, r := range doc.Rules.ContextRules {
			b.WriteString(fmt.Sprintf("%d. %s\n", i+1, r))
		}
		b.WriteString("\n")
	}
	if len(doc.Rules.ConstraintRules) > 0 {
		b.WriteString("## Constraints\n")
		for i, r := range doc.Rules.ConstraintRules {
			b.WriteString(fmt.Sprintf("%d. %s\n", i+1, r))
		}
		b.WriteString("\n")
	}
	if len(doc.Rules.WorkflowRules) > 0 {
		b.WriteString("## Workflow\n")
		for i, r := range doc.Rules.WorkflowRules {
			b.WriteString(fmt.Sprintf("%d. %s\n", i+1, r))
		}
		b.WriteString("\n")
	}
	if len(doc.Rules.AntiPatterns) > 0 {
		b.WriteString("## Anti-Patterns\n")
		for i, r := range doc.Rules.AntiPatterns {
			b.WriteString(fmt.Sprintf("%d. %s\n", i+1, r))
		}
		b.WriteString("\n")
	}

	path := filepath.Join(linksDir, doc.Hash+".md")
	return os.WriteFile(path, []byte(b.String()), 0644)
}

// extractKeywords pulls meaningful terms from the document content.
// Focuses on technical terms, tool names, and domain-specific words.
func extractKeywords(content string, rules *prompt.ParsedRules) []string {
	wordRe := regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9._-]{2,}`)
	counts := make(map[string]int)

	// Extract from all rules
	allRules := make([]string, 0, len(rules.ContextRules)+len(rules.ConstraintRules)+
		len(rules.WorkflowRules)+len(rules.AntiPatterns))
	allRules = append(allRules, rules.ContextRules...)
	allRules = append(allRules, rules.ConstraintRules...)
	allRules = append(allRules, rules.WorkflowRules...)
	allRules = append(allRules, rules.AntiPatterns...)

	for _, rule := range allRules {
		for _, word := range wordRe.FindAllString(rule, -1) {
			lower := strings.ToLower(word)
			if !isStopword(lower) {
				counts[lower]++
			}
		}
	}

	// Also extract from section headers (high signal)
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") || strings.HasPrefix(trimmed, "# ") {
			header := strings.TrimLeft(trimmed, "# ")
			for _, word := range wordRe.FindAllString(header, -1) {
				lower := strings.ToLower(word)
				if !isStopword(lower) {
					counts[lower] += 3 // boost header terms
				}
			}
		}
	}

	// Sort by frequency, take top 15
	type kv struct {
		word  string
		count int
	}
	var pairs []kv
	for w, c := range counts {
		pairs = append(pairs, kv{w, c})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].count != pairs[j].count {
			return pairs[i].count > pairs[j].count
		}
		return pairs[i].word < pairs[j].word
	})

	maxKeywords := 15
	if len(pairs) < maxKeywords {
		maxKeywords = len(pairs)
	}
	keywords := make([]string, maxKeywords)
	for i := 0; i < maxKeywords; i++ {
		keywords[i] = pairs[i].word
	}
	return keywords
}

// extractSummary generates a one-line summary from the document.
// Uses the first # header's content, or the first non-empty non-header line.
func extractSummary(content, relPath string) string {
	lines := strings.Split(content, "\n")

	// Try first # header (not ##)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") && !strings.HasPrefix(trimmed, "## ") {
			summary := strings.TrimPrefix(trimmed, "# ")
			if len(summary) > 100 {
				summary = summary[:97] + "..."
			}
			return summary
		}
	}

	// Collect ## section names as summary
	var sections []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			sections = append(sections, strings.TrimPrefix(trimmed, "## "))
		}
	}
	if len(sections) > 0 {
		summary := strings.Join(sections, ", ")
		if len(summary) > 100 {
			summary = summary[:97] + "..."
		}
		return summary
	}

	// Fallback: use the relative path
	return relPath
}

// isStopword returns true for common English words that aren't useful as keywords.
var stopwords = map[string]bool{
	"the": true, "and": true, "for": true, "are": true, "but": true,
	"not": true, "you": true, "all": true, "can": true, "has": true,
	"her": true, "was": true, "one": true, "our": true, "out": true,
	"use": true, "this": true, "that": true, "with": true, "have": true,
	"from": true, "they": true, "been": true, "will": true, "when": true,
	"what": true, "your": true, "each": true, "make": true, "like": true,
	"than": true, "them": true, "then": true, "more": true, "some": true,
	"only": true, "into": true, "other": true, "which": true, "their": true,
	"about": true, "would": true, "there": true, "these": true, "should": true,
	"does": true, "don": true, "before": true, "after": true, "instead": true,
	"using": true, "used": true, "always": true, "never": true, "must": true,
	"prefer": true, "avoid": true, "first": true, "every": true,
}

func isStopword(word string) bool {
	return stopwords[word] || len(word) <= 2
}
