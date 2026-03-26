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
	RecentCommits []string
	DiffStat      string
}

func NewContextProvider(repoDir string) *ContextProvider {
	return &ContextProvider{RepoDir: repoDir}
}

// GetContext gathers branch, recent commits, and diff stat from the repo.
// Returns an empty context (not an error) if not in a git repo.
func (g *ContextProvider) GetContext() (*Context, error) {
	ctx := &Context{}

	branch, err := g.run("branch", "--show-current")
	if err != nil {
		return ctx, nil // Not a git repo, return empty
	}
	ctx.Branch = strings.TrimSpace(branch)

	log, err := g.run("log", "--oneline", "-5")
	if err == nil {
		for _, line := range strings.Split(strings.TrimSpace(log), "\n") {
			if line != "" {
				ctx.RecentCommits = append(ctx.RecentCommits, line)
			}
		}
	}

	diff, err := g.run("diff", "--stat", "HEAD~1")
	if err == nil {
		ctx.DiffStat = strings.TrimSpace(diff)
	}

	return ctx, nil
}

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
	if c.DiffStat != "" {
		fmt.Fprintf(&b, "Recent changes:\n%s\n", c.DiffStat)
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
