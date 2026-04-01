package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Classifier enriches documents with LLM-generated metadata.
// It uses a simple Chat interface so it can work with any LLM client.
type Classifier struct {
	chat        ChatFunc
	linksDir    string
	temperature float32
	maxTokens   int
}

// ChatFunc is the function signature for a simple (non-streaming) LLM chat call.
type ChatFunc func(ctx context.Context, system, user string, temperature float32, maxTokens int) (string, error)

// ClassifyResult holds LLM-generated metadata for a document.
type ClassifyResult struct {
	Summary  string   `json:"summary"`
	Keywords []string `json:"keywords"`
	Scope    string   `json:"scope"` // "global" or "directory-specific"
}

const classifySentinel = ".classify-done"

const classifySystemPrompt = `You are a project rules classifier. Given the content of a project rules file (like CLAUDE.md), produce a JSON object with:

1. "summary": A one-line summary (max 80 chars) describing what this file covers. Be specific — mention the technologies, components, or domains.
2. "keywords": An array of 5-15 technical terms that someone searching for these rules would use. Include tool names, framework names, language names, and domain-specific terms.
3. "scope": Either "global" (applies to entire project) or "directory-specific" (applies only to the subdirectory where this file lives).

Output ONLY valid JSON. No markdown fences, no explanation.

Example output:
{"summary": "Web frontend: React 19 patterns, Tailwind styling, Base-UI components", "keywords": ["react", "typescript", "tailwind", "vite", "base-ui", "zustand", "shadcn"], "scope": "directory-specific"}`

// NewClassifier creates a Classifier with the given chat function.
func NewClassifier(chat ChatFunc, linksDir string, temperature float32, maxTokens int) *Classifier {
	return &Classifier{
		chat:        chat,
		linksDir:    linksDir,
		temperature: temperature,
		maxTokens:   maxTokens,
	}
}

// ClassifyAsync starts background LLM classification of all documents.
// Returns a channel that closes when classification is done (or skipped).
// The caller should not wait on this channel if it needs to return quickly.
func (c *Classifier) ClassifyAsync(ctx context.Context, docs []*Document) <-chan struct{} {
	done := make(chan struct{})

	go func() {
		defer close(done)

		start := time.Now()
		classified := 0

		for _, doc := range docs {
			select {
			case <-ctx.Done():
				slog.Debug("classify: context cancelled", "classified", classified)
				return
			default:
			}

			result, err := c.ClassifyOne(ctx, doc)
			if err != nil {
				slog.Warn("classify: failed for document", "source", doc.Source, "error", err)
				continue
			}

			// Enrich document with LLM results
			if result.Summary != "" {
				doc.Summary = result.Summary
			}
			if len(result.Keywords) > 0 {
				doc.Keywords = mergeKeywords(doc.Keywords, result.Keywords)
			}

			// Re-write the document file with enriched metadata
			if err := WriteDocument(doc, c.linksDir); err != nil {
				slog.Warn("classify: failed to update document", "source", doc.Source, "error", err)
			}

			classified++
			slog.Debug("classify: enriched document", "source", doc.Source, "summary", result.Summary)
		}

		if classified == 0 {
			slog.Debug("classify: no documents classified, skipping index update")
			return
		}

		// Rebuild index.json with enriched metadata
		pm := BuildMap(docs)
		if err := WriteMap(pm, c.linksDir); err != nil {
			slog.Warn("classify: failed to update project map", "error", err)
		}

		// Write sentinel file only on successful classification
		sentinelPath := filepath.Join(c.linksDir, classifySentinel)
		os.WriteFile(sentinelPath, []byte(fmt.Sprintf("%d", time.Now().Unix())), 0644)

		durationMs := time.Since(start).Milliseconds()
		slog.Debug("classify: complete", "classified", classified, "total", len(docs), "duration_ms", durationMs)
		fmt.Fprintf(os.Stderr, "restruct: classify complete — %d/%d documents, %dms\n",
			classified, len(docs), durationMs)
	}()

	return done
}

// ClassifyOne sends a single document to the LLM for enrichment.
func (c *Classifier) ClassifyOne(ctx context.Context, doc *Document) (*ClassifyResult, error) {
	userMsg := fmt.Sprintf("File: %s\n\nContent:\n%s", doc.Source, doc.Content)

	// Cap content to avoid overwhelming the LLM
	if len(userMsg) > 4000 {
		userMsg = userMsg[:4000] + "\n\n[truncated]"
	}

	raw, err := c.chat(ctx, classifySystemPrompt, userMsg, c.temperature, c.maxTokens)
	if err != nil {
		return nil, fmt.Errorf("LLM chat: %w", err)
	}

	result, err := parseClassifyResult(raw)
	if err != nil {
		return nil, fmt.Errorf("parse LLM output: %w", err)
	}

	return result, nil
}

// IsClassified checks if classification has completed (sentinel file exists).
func IsClassified(linksDir string) bool {
	_, err := os.Stat(filepath.Join(linksDir, classifySentinel))
	return err == nil
}

// ClearClassified removes the sentinel file (used when re-bootstrapping).
func ClearClassified(linksDir string) {
	os.Remove(filepath.Join(linksDir, classifySentinel))
}

// parseClassifyResult extracts a ClassifyResult from raw LLM output.
// Handles markdown fences, leading/trailing text, and malformed JSON.
func parseClassifyResult(raw string) (*ClassifyResult, error) {
	raw = strings.TrimSpace(raw)

	// Strip markdown fences if present
	if strings.HasPrefix(raw, "```") {
		lines := strings.Split(raw, "\n")
		var jsonLines []string
		inFence := false
		for _, line := range lines {
			if strings.HasPrefix(line, "```") {
				inFence = !inFence
				continue
			}
			if inFence {
				jsonLines = append(jsonLines, line)
			}
		}
		raw = strings.Join(jsonLines, "\n")
	}

	// Try to find JSON object in the output
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end > start {
		raw = raw[start : end+1]
	}

	var result ClassifyResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w (raw: %s)", err, truncate(raw, 200))
	}

	return &result, nil
}

// mergeKeywords combines structural and LLM keywords, deduplicating.
func mergeKeywords(structural, llm []string) []string {
	seen := make(map[string]bool)
	var merged []string

	// LLM keywords first (higher quality)
	for _, kw := range llm {
		lower := strings.ToLower(strings.TrimSpace(kw))
		if lower != "" && !seen[lower] {
			seen[lower] = true
			merged = append(merged, lower)
		}
	}

	// Then structural keywords
	for _, kw := range structural {
		lower := strings.ToLower(strings.TrimSpace(kw))
		if lower != "" && !seen[lower] {
			seen[lower] = true
			merged = append(merged, lower)
		}
	}

	// Cap at 15
	if len(merged) > 15 {
		merged = merged[:15]
	}
	return merged
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
