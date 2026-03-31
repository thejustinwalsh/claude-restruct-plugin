package permit

import "testing"

func TestClassifyBash_ReadOnly(t *testing.T) {
	tests := []string{
		"ls -la",
		"cat file.go",
		"grep -r pattern .",
		"git status",
		"git log --oneline -5",
		"git diff HEAD",
		"go test ./...",
		"go vet ./...",
		"find . -name '*.go'",
		"wc -l file.go",
		"jq '.name' package.json",
		"echo hello",
	}

	for _, cmd := range tests {
		t.Run(cmd, func(t *testing.T) {
			tokens := TokenizeBash(cmd)
			bc := ClassifyBash(tokens, cmd)
			if !bc.IsReadOnly {
				t.Errorf("ClassifyBash(%q) IsReadOnly = false, want true", cmd)
			}
		})
	}
}

func TestClassifyBash_Write(t *testing.T) {
	tests := []string{
		"rm file.go",
		"mkdir -p src/new",
		"mv old.go new.go",
		"cp template.go copy.go",
		"git add .",
		"git commit -m 'fix'",
		"go build ./...",
		"go mod tidy",
		"pnpm install",
		"npm install express",
	}

	for _, cmd := range tests {
		t.Run(cmd, func(t *testing.T) {
			tokens := TokenizeBash(cmd)
			bc := ClassifyBash(tokens, cmd)
			if !bc.IsWrite {
				t.Errorf("ClassifyBash(%q) IsWrite = false, want true", cmd)
			}
		})
	}
}

func TestClassifyBash_Network(t *testing.T) {
	tests := []string{
		"curl https://api.github.com/repos",
		"wget https://example.com/file.tar.gz",
		"git push origin main",
		"git clone https://github.com/user/repo",
	}

	for _, cmd := range tests {
		t.Run(cmd, func(t *testing.T) {
			tokens := TokenizeBash(cmd)
			bc := ClassifyBash(tokens, cmd)
			if !bc.IsNetwork {
				t.Errorf("ClassifyBash(%q) IsNetwork = false, want true", cmd)
			}
		})
	}
}

func TestClassifyBash_Destructive(t *testing.T) {
	tests := []string{
		"rm -rf /",
		"rm -rf ~",
		"rm -rf $HOME",
	}

	for _, cmd := range tests {
		t.Run(cmd, func(t *testing.T) {
			tokens := TokenizeBash(cmd)
			bc := ClassifyBash(tokens, cmd)
			if !bc.IsDestructive {
				t.Errorf("ClassifyBash(%q) IsDestructive = false, want true", cmd)
			}
		})
	}
}

func TestClassifyBash_Unclassifiable(t *testing.T) {
	tests := []string{
		"eval 'echo hello'",
		"source ~/.bashrc",
		"python3 -c 'import os; os.remove(\"f\")'",
		"echo $(dangerous)",
	}

	for _, cmd := range tests {
		t.Run(cmd, func(t *testing.T) {
			tokens := TokenizeBash(cmd)
			bc := ClassifyBash(tokens, cmd)
			if !bc.Unclassifiable {
				t.Errorf("ClassifyBash(%q) Unclassifiable = false, want true", cmd)
			}
		})
	}
}

func TestClassifyBash_SedInPlace(t *testing.T) {
	tokens := TokenizeBash("sed -i 's/foo/bar/g' file.txt")
	bc := ClassifyBash(tokens, "sed -i 's/foo/bar/g' file.txt")
	if !bc.IsWrite {
		t.Error("sed -i should be classified as write")
	}
}

func TestClassifyBash_SedReadOnly(t *testing.T) {
	tokens := TokenizeBash("sed 's/foo/bar/g' file.txt")
	bc := ClassifyBash(tokens, "sed 's/foo/bar/g' file.txt")
	if !bc.IsReadOnly {
		t.Error("sed without -i should be read-only")
	}
}

func TestClassifyBash_Redirection(t *testing.T) {
	cmd := "echo hello > output.txt"
	tokens := TokenizeBash(cmd)
	bc := ClassifyBash(tokens, cmd)
	if !bc.IsWrite {
		t.Error("command with > should be classified as write")
	}
	if !bc.HasRedirection {
		t.Error("HasRedirection should be true")
	}
}

func TestClassifyBash_CompoundPipe(t *testing.T) {
	cmd := "cat file.go | grep TODO | wc -l"
	tokens := TokenizeBash(cmd)
	bc := ClassifyBash(tokens, cmd)
	if !bc.IsReadOnly {
		t.Error("pipe of read-only commands should be read-only")
	}
}

func TestClassifyBash_ExtractsURLs(t *testing.T) {
	cmd := "curl https://api.example.com/data"
	tokens := TokenizeBash(cmd)
	bc := ClassifyBash(tokens, cmd)
	if len(bc.URLs) != 1 || bc.URLs[0] != "https://api.example.com/data" {
		t.Errorf("URLs = %v, want [https://api.example.com/data]", bc.URLs)
	}
}

func TestClassifyBash_ExtractsPaths(t *testing.T) {
	cmd := "rm src/old.go src/stale.go"
	tokens := TokenizeBash(cmd)
	bc := ClassifyBash(tokens, cmd)
	if len(bc.Paths) != 2 {
		t.Errorf("Paths = %v, want 2 paths", bc.Paths)
	}
}
