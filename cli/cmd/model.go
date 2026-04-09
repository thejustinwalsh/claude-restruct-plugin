package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/ollama/ollama/api"
	"github.com/spf13/cobra"
	"github.com/tjw/restruct/internal/config"
	"github.com/tjw/restruct/internal/ollama"
)

var modelCmd = &cobra.Command{
	Use:   "model",
	Short: "Manage the local LLM model",
}

var modelPullCmd = &cobra.Command{
	Use:   "pull [model]",
	Short: "Pull the configured model (or a specific one) from Ollama",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _ := config.LoadFromViper()
		if cfg == nil {
			cfg = config.Defaults()
		}

		model := cfg.Ollama.Model
		if len(args) > 0 {
			model = args[0]
		}

		client, err := ollama.NewClient(cfg.Ollama.URL, model, cfg.Ollama.ConnectTimeout, cfg.Ollama.RequestTimeout, cfg.Ollama.StallTimeout, cfg.Ollama.KeepAlive)
		if err != nil {
			return jsonError("client_init", err)
		}

		ctx := context.Background()
		if !client.IsAvailable(ctx) {
			return jsonError("ollama_unavailable", fmt.Errorf("ollama is not running at %s", cfg.Ollama.URL))
		}

		fmt.Fprintf(os.Stderr, "Pulling %s...\n", model)
		start := time.Now()

		err = client.PullModel(ctx, model, func(resp api.ProgressResponse) {
			if resp.Total > 0 {
				pct := float64(resp.Completed) / float64(resp.Total) * 100
				fmt.Fprintf(os.Stderr, "\r  %s: %.0f%%", resp.Status, pct)
			} else if resp.Status != "" {
				fmt.Fprintf(os.Stderr, "\r  %s", resp.Status)
			}
		})
		fmt.Fprintln(os.Stderr)

		if err != nil {
			return jsonError("pull_failed", err)
		}

		result := map[string]any{
			"ok":       true,
			"model":    model,
			"duration": time.Since(start).Round(time.Second).String(),
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	},
}

var modelLoadCmd = &cobra.Command{
	Use:   "load [model]",
	Short: "Preload model into memory with configured keep_alive",
	Long:  `Sends a warm-up request to load the model into GPU/RAM and sets keep_alive so it stays resident.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := loadConfigOrDefaults()

		if !cfg.RefinementEnabled() {
			fmt.Fprintln(cmd.ErrOrStderr(), "restruct: model load skipped — refinement feature not yet enabled")
			return nil
		}

		model := cfg.Ollama.Model
		if len(args) > 0 {
			model = args[0]
		}

		keepAlive := cfg.Ollama.KeepAlive
		if ka, _ := cmd.Flags().GetDuration("keep-alive"); ka > 0 {
			keepAlive = ka
		}

		client, err := ollama.NewClient(cfg.Ollama.URL, model, cfg.Ollama.ConnectTimeout, 5*time.Minute, cfg.Ollama.StallTimeout, keepAlive)
		if err != nil {
			return jsonError("client_init", err)
		}

		ctx := context.Background()
		if !client.IsAvailable(ctx) {
			return jsonError("ollama_unavailable", fmt.Errorf("ollama is not running at %s", cfg.Ollama.URL))
		}

		fmt.Fprintf(os.Stderr, "Loading %s (keep_alive=%s)...\n", model, keepAlive)
		start := time.Now()

		if err := client.LoadModel(ctx, model, keepAlive); err != nil {
			return jsonError("load_failed", err)
		}

		result := map[string]any{
			"ok":         true,
			"model":      model,
			"keep_alive": keepAlive.String(),
			"duration":   time.Since(start).Round(time.Millisecond).String(),
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	},
}

var modelStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check which models are currently loaded in memory",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _ := config.LoadFromViper()
		if cfg == nil {
			cfg = config.Defaults()
		}

		client, err := ollama.NewClient(cfg.Ollama.URL, cfg.Ollama.Model, cfg.Ollama.ConnectTimeout, cfg.Ollama.RequestTimeout, cfg.Ollama.StallTimeout, cfg.Ollama.KeepAlive)
		if err != nil {
			return jsonError("client_init", err)
		}

		ctx := context.Background()
		if !client.IsAvailable(ctx) {
			return jsonError("ollama_unavailable", fmt.Errorf("ollama is not running at %s", cfg.Ollama.URL))
		}

		models, err := client.ListModels(ctx)
		if err != nil {
			return jsonError("list_failed", err)
		}

		type modelInfo struct {
			Name   string `json:"name"`
			SizeGB string `json:"size_gb"`
		}
		var list []modelInfo
		for _, m := range models {
			sizeGB := float64(m.Size) / (1024 * 1024 * 1024)
			list = append(list, modelInfo{
				Name:   m.Name,
				SizeGB: fmt.Sprintf("%.1f", sizeGB),
			})
		}

		result := map[string]any{
			"configured_model": cfg.Ollama.Model,
			"available_models": list,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	},
}

func init() {
	rootCmd.AddCommand(modelCmd)
	modelCmd.AddCommand(modelPullCmd)
	modelCmd.AddCommand(modelLoadCmd)
	modelCmd.AddCommand(modelStatusCmd)

	modelLoadCmd.Flags().Duration("keep-alive", 0, "Override keep_alive duration (default from config: 60m)")
}

func jsonError(code string, err error) error {
	result := map[string]any{
		"ok":    false,
		"error": code,
		"detail": err.Error(),
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(result)
	return err
}
