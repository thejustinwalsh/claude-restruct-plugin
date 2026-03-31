package permit

import "testing"

func TestClassifyTool(t *testing.T) {
	tests := []struct {
		name     string
		tool     string
		input    map[string]any
		category ToolCategory
	}{
		{"Read", "Read", map[string]any{"file_path": "/p/f.go"}, CategoryReadOnly},
		{"Glob", "Glob", map[string]any{"path": "/p"}, CategoryReadOnly},
		{"Grep", "Grep", map[string]any{"path": "/p"}, CategoryReadOnly},
		{"Write", "Write", map[string]any{"file_path": "/p/f.go"}, CategoryWrite},
		{"Edit", "Edit", map[string]any{"file_path": "/p/f.go"}, CategoryWrite},
		{"Bash", "Bash", map[string]any{"command": "ls"}, CategoryBash},
		{"WebFetch", "WebFetch", map[string]any{"url": "https://x.com"}, CategoryNetwork},
		{"WebSearch", "WebSearch", map[string]any{"query": "go testing"}, CategoryNetwork},
		{"ToolSearch", "ToolSearch", map[string]any{}, CategoryAlwaysAllow},
		{"Agent", "Agent", map[string]any{}, CategoryUnknown},
		{"Unknown", "FooTool", map[string]any{}, CategoryUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := ClassifyTool(tt.tool, tt.input)
			if tc.Category != tt.category {
				t.Errorf("ClassifyTool(%q) category = %d, want %d", tt.tool, tc.Category, tt.category)
			}
		})
	}
}

func TestClassifyTool_ExtractsPaths(t *testing.T) {
	tc := ClassifyTool("Read", map[string]any{"file_path": "/project/main.go"})
	if len(tc.Paths) != 1 || tc.Paths[0] != "/project/main.go" {
		t.Errorf("paths = %v, want [/project/main.go]", tc.Paths)
	}
}

func TestClassifyTool_ExtractsURLs(t *testing.T) {
	tc := ClassifyTool("WebFetch", map[string]any{"url": "https://api.github.com/repos"})
	if len(tc.URLs) != 1 || tc.URLs[0] != "https://api.github.com/repos" {
		t.Errorf("urls = %v, want [https://api.github.com/repos]", tc.URLs)
	}
}

func TestClassifyTool_MissingFields(t *testing.T) {
	tc := ClassifyTool("Read", map[string]any{})
	if len(tc.Paths) != 0 {
		t.Errorf("paths should be empty for missing input, got %v", tc.Paths)
	}
}
