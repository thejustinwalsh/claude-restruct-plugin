package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	Version = "dev"
	cfgFile string
)

var rootCmd = &cobra.Command{
	Use:   "restruct",
	Short: "Meta-prompt hook system for Claude Code",
	Long: `Restruct intercepts conversational prompts and transforms them into
structured, rules-aware instructions via a local LLM before Claude sees them.`,
	Version: Version,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default $HOME/.config/restruct/config.yaml)")
	rootCmd.PersistentFlags().String("ollama-url", "http://localhost:11434", "Ollama API base URL")
	rootCmd.PersistentFlags().String("model", "qwen2.5-coder:14b", "Local LLM model name")
	rootCmd.PersistentFlags().Bool("verbose", false, "Enable verbose output to stderr")
	rootCmd.PersistentFlags().Duration("timeout", 0, "Ollama request timeout (default 10s)")

	viper.BindPFlag("ollama.url", rootCmd.PersistentFlags().Lookup("ollama-url"))
	viper.BindPFlag("ollama.model", rootCmd.PersistentFlags().Lookup("model"))
	viper.BindPFlag("ollama.timeout", rootCmd.PersistentFlags().Lookup("timeout"))
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return
		}
		viper.AddConfigPath(home + "/.config/restruct")
		viper.AddConfigPath(".")
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
	}

	viper.SetEnvPrefix("RESTRUCT")
	viper.AutomaticEnv()
	viper.ReadInConfig() // Silently ignore if missing
}
