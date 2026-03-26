package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage restruct configuration",
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a configuration value",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		viper.Set(args[0], args[1])
		cfgPath := viper.ConfigFileUsed()
		if cfgPath == "" {
			home, _ := os.UserHomeDir()
			cfgPath = home + "/.config/restruct/config.yaml"
			os.MkdirAll(home+"/.config/restruct", 0755)
		}
		if err := viper.WriteConfigAs(cfgPath); err != nil {
			return fmt.Errorf("write config: %w", err)
		}
		fmt.Printf("%s = %s\n", args[0], args[1])
		return nil
	},
}

var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Get a configuration value",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		val := viper.Get(args[0])
		if val == nil {
			fmt.Fprintf(os.Stderr, "key %q not set\n", args[0])
			os.Exit(1)
		}
		fmt.Println(val)
	},
}

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configuration values",
	Run: func(cmd *cobra.Command, args []string) {
		keys := viper.AllKeys()
		sort.Strings(keys)
		for _, k := range keys {
			if !strings.HasPrefix(k, "_") {
				fmt.Printf("%s = %v\n", k, viper.Get(k))
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configListCmd)
}
