package git

import (
	"fmt"
	"os/exec"
	"strings"
)

type ContextProvider struct {
	RepoDir string
}

type Context struct {
	Branch        string
	RecentCommits []string // oneline format: "hash message"
}

func NewContextProvider(repoDir string) *ContextProvider {
	return &ContextProvider{RepoDir: repoDir}
}

// GetContext gathers branch and recent commit messages.
// Returns an empty context (not an error) if not in a git repo.
func (g *ContextProvider) GetContext() (*Context, error) {
	branch, err := g.run("branch", "--show-current")
	if err != nil {
		return &Context{}, nil
	}

	ctx := &Context{Branch: strings.TrimSpace(branch)}

	log, err := g.run("log", "--oneline", "-5")
	if err == nil {
		for _, line := range strings.Split(strings.TrimSpace(log), "\n") {
			if line != "" {
				ctx.RecentCommits = append(ctx.RecentCommits, line)
			}
		}
	}

	return ctx, nil
}

// String formats the context for the local LLM's user message.
func (c *Context) String() string {
	if c.Branch == "" {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Branch: %s\n", c.Branch)
	if len(c.RecentCommits) > 0 {
		b.WriteString("Recent commits:\n")
		for _, c := range c.RecentCommits {
			fmt.Fprintf(&b, "  %s\n", c)
		}
	}
	return b.String()
}

func (g *ContextProvider) run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = g.RepoDir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
