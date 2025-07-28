package main

import (
	"bytes"
	"log"
	"log/slog"
	"strings"
	"testing"

	"github.com/l7mp/stunner"
)

func TestStunnerWrapperLogging(t *testing.T) {
	// Create a buffer to capture log output
	var buf bytes.Buffer

	// Set up slog with JSON handler that writes to buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
		AddSource: true,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.SourceKey {
				return slog.String("component", "stunner")
			}
			return a
		},
	})

	// THE KEY: Redirect standard log to slog
	logWriter := setupSlogRedirect(handler)
	
	log.SetFlags(0)
	log.SetOutput(logWriter)

	// Create Stunner instance with dry-run mode
	options := stunner.Options{
		LogLevel:         "all:DEBUG",
		DryRun:           true,
		SuppressRollback: true,
	}

	s := stunner.NewStunner(options)
	if s == nil {
		t.Fatal("Failed to create STUNner instance")
	}

	// Create a simple configuration to trigger some logging
	conf, err := stunner.NewDefaultConfig("turn://user:pass@127.0.0.1:3478")
	if err != nil {
		t.Fatalf("Failed to create default config: %v", err)
	}

	// Reconcile the configuration (this will generate logs)
	err = s.Reconcile(conf)
	if err != nil {
		t.Fatalf("Failed to reconcile: %v", err)
	}

	// Close Stunner
	s.Close()

	// Get the captured log output
	logOutput := buf.String()

	// Analyze the output
	lines := strings.Split(strings.TrimSpace(logOutput), "\n")
	
	jsonCount := 0
	nonJsonCount := 0
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		
		// Check if line is JSON (starts with {)
		if strings.HasPrefix(line, "{") {
			jsonCount++
		} else {
			nonJsonCount++
		}
	}

	t.Logf("Total log lines: %d", len(lines))
	t.Logf("JSON formatted logs: %d", jsonCount)
	t.Logf("Non-JSON logs: %d", nonJsonCount)

	// Verify that we have JSON logs
	if jsonCount == 0 {
		t.Error("No JSON logs found - wrapper is not working")
	} else {
		t.Logf("✅ SUCCESS: Found %d JSON logs", jsonCount)
	}

	// Show some sample logs
	t.Log("\nSample log output:")
	for i, line := range lines {
		if i < 5 && strings.TrimSpace(line) != "" { // Show first 5 non-empty lines
			t.Logf("  %s", line)
		}
	}
}

func TestStunnerWrapperWithRealConfig(t *testing.T) {
	// Create a buffer to capture log output
	var buf bytes.Buffer

	// Set up slog with JSON handler
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
		AddSource: true,
	})

	// Redirect standard log to slog
	logWriter := setupSlogRedirect(handler)
	
	log.SetFlags(0)
	log.SetOutput(logWriter)

	// Create Stunner with more verbose logging
	options := stunner.Options{
		LogLevel:         "all:TRACE",
		DryRun:           true,
		SuppressRollback: true,
	}

	s := stunner.NewStunner(options)
	if s == nil {
		t.Fatal("Failed to create STUNner instance")
	}

	// Create a configuration that will generate various log messages
	conf, err := stunner.NewDefaultConfig("turn://user:pass@127.0.0.1:3478")
	if err != nil {
		t.Fatalf("Failed to create default config: %v", err)
	}

	// Set log level to generate more logs
	conf.Admin.LogLevel = "all:DEBUG"

	// Reconcile
	err = s.Reconcile(conf)
	if err != nil {
		t.Fatalf("Failed to reconcile: %v", err)
	}

	// Get status to trigger more logging
	status := s.Status()
	t.Logf("Stunner status: %s", status.String())

	// Close
	s.Close()

	// Analyze results
	logOutput := buf.String()
	lines := strings.Split(strings.TrimSpace(logOutput), "\n")
	
	jsonCount := 0
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "{") {
			jsonCount++
		}
	}

	t.Logf("✅ Generated %d JSON log entries", jsonCount)
	
	if jsonCount > 0 {
		t.Log("✅ SUCCESS: Stunner wrapper successfully converts logs to JSON format")
	} else {
		t.Error("❌ FAILURE: No JSON logs generated")
	}
} 