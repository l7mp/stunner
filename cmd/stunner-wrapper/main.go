package main

import (
	"context"
	"flag"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/l7mp/stunner"
)

func main() {
	// Parse command line flags
	var (
		logLevel = flag.String("loglevel", "all:INFO", "Log level specification")
		dryRun   = flag.Bool("dry-run", false, "Dry run mode")
		name     = flag.String("name", "", "Stunner instance name")
	)
	flag.Parse()

	// Set up slog with JSON handler
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
		AddSource: true,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Add component field to identify logs from Stunner
			if a.Key == slog.SourceKey {
				return slog.String("component", "stunner")
			}
			return a
		},
	})

	// THE KEY: Redirect standard log to slog
	logWriter := setupSlogRedirect(handler)
	
	log.SetFlags(0) // Remove default flags since slog will handle formatting
	log.SetOutput(logWriter)

	// Log that we're starting the wrapper
	slog.Info("Starting STUNner with JSON logging wrapper", 
		"log_level", *logLevel,
		"dry_run", *dryRun,
		"name", *name)

	// Create Stunner options
	options := stunner.Options{
		LogLevel:         *logLevel,
		DryRun:           *dryRun,
		SuppressRollback: false,
	}

	if *name != "" {
		options.Name = *name
	}

	// Create Stunner instance
	// All logging from Stunner will now go through slog JSON handler
	s := stunner.NewStunner(options)
	if s == nil {
		slog.Error("Failed to create STUNner instance")
		os.Exit(1)
	}

	// Log Stunner instance info
	slog.Info("STUNner instance created successfully",
		"instance_id", s.GetId(),
		"version", s.GetVersion())

	// Set up graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		slog.Info("Received shutdown signal", "signal", sig.String())
		cancel()
	}()

	// Keep the server running until context is cancelled
	<-ctx.Done()

	// Shutdown gracefully
	slog.Info("Shutting down STUNner")
	s.Close()
	slog.Info("STUNner shutdown complete")
} 