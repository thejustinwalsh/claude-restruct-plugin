package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tjw/restruct/internal/db"
	"github.com/tjw/restruct/internal/toggle"
)

var enableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable restruct prompt refinement",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := toggle.Enable(db.DataDir()); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStderr(), "restruct: enabled")
		return nil
	},
}

var disableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable restruct prompt refinement (passthrough all prompts)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := toggle.Disable(db.DataDir()); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStderr(), "restruct: disabled — all prompts will pass through unrefined")
		fmt.Fprintln(cmd.OutOrStderr(), "restruct: run 'restruct enable' to re-enable")
		return nil
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show whether restruct is enabled or disabled",
	RunE: func(cmd *cobra.Command, args []string) error {
		if toggle.IsEnabled(db.DataDir()) {
			fmt.Fprintln(cmd.OutOrStderr(), "restruct: enabled")
		} else {
			fmt.Fprintln(cmd.OutOrStderr(), "restruct: disabled")
			fmt.Fprintln(cmd.OutOrStderr(), "restruct: run 'restruct enable' to re-enable")
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(enableCmd)
	rootCmd.AddCommand(disableCmd)
	rootCmd.AddCommand(statusCmd)
}
