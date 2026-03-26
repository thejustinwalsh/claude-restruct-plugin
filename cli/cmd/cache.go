package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tjw/restruct/internal/cache"
	"github.com/tjw/restruct/internal/config"
)

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Manage the prompt refinement cache",
}

var cacheClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear all cached refined prompts",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _ := config.LoadFromViper()
		if cfg == nil {
			cfg = config.Defaults()
		}
		s := cache.NewStore(cfg.Cache.Dir, true)
		if err := s.Clear(); err != nil {
			return fmt.Errorf("clear cache: %w", err)
		}
		fmt.Println("Cache cleared.")
		return nil
	},
}

var cacheStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show cache statistics",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _ := config.LoadFromViper()
		if cfg == nil {
			cfg = config.Defaults()
		}
		s := cache.NewStore(cfg.Cache.Dir, true)
		entries, size, err := s.Stats()
		if err != nil {
			return fmt.Errorf("cache stats: %w", err)
		}
		fmt.Printf("Entries: %d\nSize: %d bytes\n", entries, size)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(cacheCmd)
	cacheCmd.AddCommand(cacheClearCmd)
	cacheCmd.AddCommand(cacheStatsCmd)
}
