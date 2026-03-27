package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/tjw/restruct/internal/db"
	"github.com/tjw/restruct/internal/server"
)

var serveCmd = &cobra.Command{
	Use:           "serve",
	Short:         "Start the restruct dashboard server",
	Long:          `Starts the web dashboard for monitoring refinements, sessions, and pipeline telemetry.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		port, _ := cmd.Flags().GetString("port")
		dev := server.IsDevMode()
		daemon, _ := cmd.Flags().GetBool("daemon")
		verbose, _ := cmd.Root().PersistentFlags().GetBool("verbose")

		logLevel := slog.LevelInfo
		if verbose {
			logLevel = slog.LevelDebug
		}
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})))

		if daemon {
			return daemonize(port)
		}

		// Open database
		database, err := db.Open(db.DefaultPath())
		if err != nil {
			return fmt.Errorf("open database: %w", err)
		}
		defer database.Close()

		// Create server — WebDist() returns nil in debug builds (proxies to Vite)
		srv := server.New(database, port, dev, server.WebDist())

		// Handle shutdown
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigCh
			slog.Info("shutting down...")
			cancel()
			srv.Shutdown(context.Background())
		}()

		// Write PID file
		pidPath := pidFilePath()
		os.MkdirAll(filepath.Dir(pidPath), 0755)
		os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0644)
		defer os.Remove(pidPath)

		if dev {
			slog.Info("dev mode: CORS enabled for localhost:5173")
		}

		err = srv.Start(ctx)
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	},
}

var serveStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the running dashboard server",
	RunE: func(cmd *cobra.Command, args []string) error {
		pid, err := readPID()
		if err != nil {
			return fmt.Errorf("no running server found: %w", err)
		}

		proc, err := os.FindProcess(pid)
		if err != nil {
			return fmt.Errorf("find process %d: %w", pid, err)
		}

		if err := proc.Signal(syscall.SIGTERM); err != nil {
			return fmt.Errorf("stop server (pid %d): %w", pid, err)
		}

		os.Remove(pidFilePath())
		fmt.Printf("Server stopped (PID %d)\n", pid)
		return nil
	},
}

var serveStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check if the dashboard server is running",
	RunE: func(cmd *cobra.Command, args []string) error {
		pid, err := readPID()
		if err != nil {
			fmt.Println("Server is not running")
			return nil
		}

		// Check if process is alive
		proc, err := os.FindProcess(pid)
		if err != nil {
			fmt.Println("Server is not running (stale PID file)")
			os.Remove(pidFilePath())
			return nil
		}

		// On Unix, signal 0 checks if process exists
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			fmt.Println("Server is not running (stale PID file)")
			os.Remove(pidFilePath())
			return nil
		}

		fmt.Printf("Server is running (PID %d)\n", pid)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.AddCommand(serveStopCmd)
	serveCmd.AddCommand(serveStatusCmd)

	serveCmd.Flags().String("port", "8377", "Port to listen on")
	serveCmd.Flags().Bool("daemon", false, "Run as background daemon")
}

func pidFilePath() string {
	if dir := os.Getenv("CLAUDE_PLUGIN_DATA"); dir != "" {
		return filepath.Join(dir, "restruct-server.pid")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "restruct", "restruct-server.pid")
}

func readPID() (int, error) {
	data, err := os.ReadFile(pidFilePath())
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(data))
}

func daemonize(port string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	// Re-launch self with --daemon removed, running in foreground
	attr := &os.ProcAttr{
		Dir:   ".",
		Env:   os.Environ(),
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
		Sys:   &syscall.SysProcAttr{Setsid: true},
	}

	proc, err := os.StartProcess(exe, []string{exe, "serve", "--port", port}, attr)
	if err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	fmt.Printf("Server started in background (PID %d) on port %s\n", proc.Pid, port)
	fmt.Printf("Dashboard: http://localhost:%s\n", port)
	proc.Release()
	return nil
}
