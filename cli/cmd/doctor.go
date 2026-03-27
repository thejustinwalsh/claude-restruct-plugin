package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tjw/restruct/internal/config"
	"github.com/tjw/restruct/internal/ollama"
)

// DoctorReport is the JSON output of `restruct doctor`.
type DoctorReport struct {
	OllamaInstalled  bool   `json:"ollama_installed"`
	OllamaBinaryPath string `json:"ollama_binary_path,omitempty"`
	OllamaRunning    bool   `json:"ollama_running"`
	OllamaVersion    string `json:"ollama_version,omitempty"`
	MinVersion       string `json:"min_version"`
	VersionOK        bool   `json:"version_ok"`
	ModelRequired    string `json:"model_required"`
	ModelPulled      bool   `json:"model_pulled"`
	ModelSizeGB      string `json:"model_size_gb,omitempty"`
	KeepAlive        string `json:"keep_alive"`
	ConfigPath       string `json:"config_path,omitempty"`
	AllGood          bool   `json:"all_good"`
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check system readiness and return JSON status report",
	Long:  `Checks Ollama installation, server status, model availability, and configuration. Returns a JSON report for programmatic consumption.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadFromViper()
		if err != nil {
			cfg = config.Defaults()
		}

		report := DoctorReport{
			MinVersion:    cfg.Ollama.MinVersion,
			ModelRequired: cfg.Ollama.Model,
			KeepAlive:     cfg.Ollama.KeepAlive.String(),
			ConfigPath:    configFileUsed(),
		}

		// Check ollama binary
		binPath, err := exec.LookPath("ollama")
		if err == nil {
			report.OllamaInstalled = true
			report.OllamaBinaryPath = binPath
		}

		// Check server + version + model
		ctx := context.Background()
		client, err := ollama.NewClient(cfg.Ollama.URL, cfg.Ollama.Model, cfg.Ollama.ConnectTimeout, cfg.Ollama.RequestTimeout, cfg.Ollama.StallTimeout, cfg.Ollama.KeepAlive)
		if err == nil && client.IsAvailable(ctx) {
			report.OllamaRunning = true

			if v, err := client.Version(ctx); err == nil {
				report.OllamaVersion = v
				report.VersionOK = compareVersions(v, cfg.Ollama.MinVersion)
			}

			if models, err := client.ListModels(ctx); err == nil {
				for _, m := range models {
					if matchesModel(m.Name, cfg.Ollama.Model) {
						report.ModelPulled = true
						sizeGB := float64(m.Size) / (1024 * 1024 * 1024)
						report.ModelSizeGB = fmt.Sprintf("%.1f", sizeGB)
						break
					}
				}
			}
		}

		report.AllGood = report.OllamaInstalled && report.OllamaRunning &&
			report.VersionOK && report.ModelPulled

		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	},
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

func configFileUsed() string {
	// viper.ConfigFileUsed() returns empty if no file was loaded
	f := configFileUsedFromViper()
	if f != "" {
		return f
	}
	return "(defaults — no config file found)"
}

func configFileUsedFromViper() string {
	// Best-effort; viper doesn't expose this cleanly before read
	for _, candidate := range []string{
		os.ExpandEnv("$HOME/.config/restruct/config.yaml"),
		".restruct.yaml",
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// compareVersions returns true if actual >= required (simple semver major.minor.patch).
func compareVersions(actual, required string) bool {
	parse := func(v string) (parts [3]int) {
		v = strings.TrimPrefix(v, "v")
		fmt.Sscanf(v, "%d.%d.%d", &parts[0], &parts[1], &parts[2])
		return
	}
	a := parse(actual)
	r := parse(required)
	for i := 0; i < 3; i++ {
		if a[i] > r[i] {
			return true
		}
		if a[i] < r[i] {
			return false
		}
	}
	return true // equal
}

// matchesModel checks if an installed model name matches the required one.
// Handles cases like "qwen2.5-coder:14b" matching "qwen2.5-coder:14b-instruct-q4_K_M".
func matchesModel(installed, required string) bool {
	return installed == required || strings.HasPrefix(installed, required)
}
