package main

import (
	"bytes"
	"log"
	"log/slog"
	"strings"
	"testing"
)

func TestSimpleLogRedirect(t *testing.T) {
	// Create a buffer to capture log output
	var buf bytes.Buffer

	// Set up slog with JSON handler
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	// Redirect standard log to slog
	logWriter := setupSlogRedirect(handler)
	log.SetFlags(0)
	log.SetOutput(logWriter)

	// Test with standard log calls (simulating Stunner's internal logging)
	log.Print("stunner INFO: listener default-listener (re)starting")
	log.Print("auth DEBUG: Authentication request: client=192.168.1.100:54321")
	log.Print("listener ERROR: Could not start server: connection refused")

	// Test with slog calls
	slog.Info("Test slog message", "key", "value")
	slog.Error("Test slog error", "error", "test error")

	// Get the captured log output
	logOutput := buf.String()
	
	t.Logf("Captured log output:\n%s", logOutput)
	
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
	
	if jsonCount > 0 {
		t.Log("✅ SUCCESS: Slog wrapper is working - logs are being redirected to JSON format")
	} else {
		t.Error("❌ FAILURE: No JSON logs found - slog wrapper is not working")
	}
} 