package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tjw/restruct/internal/hook"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install restruct as a Claude Code hook",
	Long: `Adds restruct as a UserPromptSubmit hook in .claude/settings.json.
Also installs SessionStart and SessionEnd hooks for session tracking,
and adds .restruct/ to .gitignore.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, _ := cmd.Flags().GetString("project-dir")
		global, _ := cmd.Flags().GetBool("global")

		if err := hook.Install(projectDir, global); err != nil {
			return fmt.Errorf("install failed: %w", err)
		}

		if global {
			fmt.Println("Hooks installed globally in ~/.claude/settings.json")
		} else {
			fmt.Println("Hooks installed in .claude/settings.json")
			fmt.Println("Added .restruct/ to .gitignore")
		}
		fmt.Println("\nInstalled hooks:")
		fmt.Println("  UserPromptSubmit → restruct refine")
		fmt.Println("  SessionStart     → restruct session start")
		fmt.Println("  SessionEnd       → restruct session end")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(installCmd)
	installCmd.Flags().String("project-dir", ".", "Project directory to install hook into")
	installCmd.Flags().Bool("global", false, "Install to ~/.claude/settings.json instead of project")
}
