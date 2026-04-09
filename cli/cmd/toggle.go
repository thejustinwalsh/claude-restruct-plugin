package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tjw/restruct/internal/config"
	"github.com/tjw/restruct/internal/db"
	"github.com/tjw/restruct/internal/toggle"
)

func loadConfigOrDefaults() *config.Config {
	cfg, err := config.LoadFromViper()
	if err != nil || cfg == nil {
		return config.Defaults()
	}
	return cfg
}

var enableCmd = &cobra.Command{
	Use:           "enable",
	Short:         "Enable restruct prompt refinement",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.GuardRefinement(loadConfigOrDefaults()); err != nil {
			// root.go's Execute() prints the error and exits non-zero.
			return err
		}
		if err := toggle.Enable(db.DataDir()); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStderr(), "restruct: enabled")
		return nil
	},
}

var disableCmd = &cobra.Command{
	Use:           "disable",
	Short:         "Disable restruct prompt refinement (passthrough all prompts)",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.GuardRefinement(loadConfigOrDefaults()); err != nil {
			return err
		}
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
		cfg := loadConfigOrDefaults()
		if !cfg.RefinementEnabled() {
			fmt.Fprintln(cmd.OutOrStderr(), "restruct: refinement feature not yet enabled in this release")
			fmt.Fprintln(cmd.OutOrStderr(), "restruct: set features.refinement: true in config.yaml to opt in")
			return nil
		}
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
