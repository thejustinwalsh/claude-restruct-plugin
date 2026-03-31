package permit

// ToolCategory classifies a tool call's nature.
type ToolCategory int

const (
	CategoryReadOnly ToolCategory = iota
	CategoryWrite
	CategoryBash
	CategoryNetwork
	CategoryAlwaysAllow // ToolSearch, etc. — metadata only
	CategoryUnknown
)

// ToolClassification is the result of classifying a tool call.
type ToolClassification struct {
	Category    ToolCategory
	Paths       []string // extracted file paths from tool_input
	URLs        []string // extracted URLs from tool_input
	Description string
}

// ClassifyTool classifies a built-in Claude Code tool by name and input.
// Returns CategoryBash for Bash tool (needs further analysis via bash.go).
func ClassifyTool(toolName string, toolInput map[string]any) ToolClassification {
	switch toolName {
	case "Read", "Glob", "Grep":
		return ToolClassification{
			Category:    CategoryReadOnly,
			Paths:       extractStringFields(toolInput, "file_path", "path"),
			Description: "Read-only file operation",
		}

	case "Write", "Edit", "NotebookEdit", "MultiEdit":
		return ToolClassification{
			Category:    CategoryWrite,
			Paths:       extractStringFields(toolInput, "file_path", "path", "notebook_path"),
			Description: "File write operation",
		}

	case "Bash":
		return ToolClassification{
			Category:    CategoryBash,
			Description: "Bash command",
		}

	case "WebFetch":
		return ToolClassification{
			Category: CategoryNetwork,
			URLs:     extractStringFields(toolInput, "url"),
			Description: "Web fetch",
		}

	case "WebSearch":
		return ToolClassification{
			Category:    CategoryNetwork,
			URLs:        extractStringFields(toolInput, "query"),
			Description: "Web search",
		}

	case "ToolSearch", "TaskList", "TaskGet", "TaskOutput":
		return ToolClassification{
			Category:    CategoryAlwaysAllow,
			Description: "Metadata-only tool",
		}

	case "Agent", "Skill", "SendMessage":
		// These spawn sub-operations with their own permission checks
		return ToolClassification{
			Category:    CategoryUnknown,
			Description: "Delegating tool",
		}

	default:
		return ToolClassification{
			Category:    CategoryUnknown,
			Description: "Unknown tool: " + toolName,
		}
	}
}

// extractStringFields extracts string values from a map for the given keys.
func extractStringFields(input map[string]any, keys ...string) []string {
	var vals []string
	for _, k := range keys {
		if v, ok := input[k].(string); ok && v != "" {
			vals = append(vals, v)
		}
	}
	return vals
}
