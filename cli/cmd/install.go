package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tjw/restruct/internal/hook"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install restruct as a Claude Code hook",
	Long:  `Adds the restruct refine command as a UserPrompt hook in .claude/settings.json.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		projectDir, _ := cmd.Flags().GetString("project-dir")
		global, _ := cmd.Flags().GetBool("global")

		if err := hook.Install(projectDir, global); err != nil {
			return fmt.Errorf("install failed: %w", err)
		}

		fmt.Println("Hook installed successfully.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(installCmd)
	installCmd.Flags().String("project-dir", ".", "Project directory to install hook into")
	installCmd.Flags().Bool("global", false, "Install to ~/.claude/settings.json instead of project")
}
