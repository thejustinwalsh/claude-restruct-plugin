package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tjw/restruct/internal/config"
	"github.com/tjw/restruct/internal/hook"
	"github.com/tjw/restruct/internal/pipeline"
)

var refineCmd = &cobra.Command{
	Use:   "refine",
	Short: "Refine a prompt via local LLM (Claude Code hook entry point)",
	Long:  `Reads a Claude Code hook JSON payload from stdin, refines the prompt, and writes the result to stdout.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		bypass, _ := cmd.Flags().GetBool("bypass")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		input, err := hook.ParseInput(os.Stdin)
		if err != nil {
			return hook.WriteOutput(os.Stdout, hook.ErrorOutput(fmt.Sprintf("parse error: %v", err)))
		}

		// Pass through short prompts or when bypassed
		if bypass || len(strings.Fields(input.Prompt)) < 5 {
			return hook.WriteOutput(os.Stdout, hook.OKOutput())
		}

		cfg, err := config.LoadFromViper()
		if err != nil {
			fmt.Fprintf(os.Stderr, "restruct: config error: %v, using defaults\n", err)
			cfg = config.Defaults()
		}

		p, err := pipeline.New(cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "restruct: pipeline init error: %v, passing through\n", err)
			return hook.WriteOutput(os.Stdout, hook.OKOutput())
		}

		refined, err := p.Refine(context.Background(), input.Prompt)
		if err != nil {
			fmt.Fprintf(os.Stderr, "restruct: refinement error: %v, passing through\n", err)
			return hook.WriteOutput(os.Stdout, hook.OKOutput())
		}

		if dryRun {
			fmt.Fprintln(os.Stderr, refined)
			return hook.WriteOutput(os.Stdout, hook.OKOutput())
		}

		return hook.WriteOutput(os.Stdout, hook.RefinedOutput(refined))
	},
}

func init() {
	rootCmd.AddCommand(refineCmd)
	refineCmd.Flags().Bool("bypass", false, "Skip refinement, pass prompt through")
	refineCmd.Flags().Bool("dry-run", false, "Print refined prompt to stderr without replacing")
}
