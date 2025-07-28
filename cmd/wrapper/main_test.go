package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/l7mp/stunner"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

func TestStunnerWrapper_SetupJSONLogging(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Create wrapper and setup JSON logging
	wrapper := NewStunnerWrapper()
	wrapper.SetupJSONLogging()

	// Log a test message
	wrapper.jsonLogger.Info("Test JSON logging")

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Read captured output
	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Verify JSON output
	if !strings.Contains(output, "Test JSON logging") {
		t.Errorf("Expected JSON output to contain test message, got: %s", output)
	}

	// Try to parse as JSON
	var logEntry map[string]interface{}
	if err := json.Unmarshal([]byte(output), &logEntry); err != nil {
		t.Errorf("Expected valid JSON output, got error: %v", err)
	}

	// Verify JSON structure
	if _, ok := logEntry["time"]; !ok {
		t.Error("Expected JSON log to have 'time' field")
	}
	if _, ok := logEntry["level"]; !ok {
		t.Error("Expected JSON log to have 'level' field")
	}
	if _, ok := logEntry["msg"]; !ok {
		t.Error("Expected JSON log to have 'msg' field")
	}
}

func TestStunnerWrapper_InitializeStunner(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Create wrapper and setup JSON logging
	wrapper := NewStunnerWrapper()
	wrapper.SetupJSONLogging()

	// Initialize Stunner
	options := stunner.Options{
		Name:     "test-stunner",
		LogLevel: "all:INFO",
		DryRun:   true,
	}

	err := wrapper.InitializeStunner(options)
	if err != nil {
		t.Errorf("Failed to initialize Stunner: %v", err)
	}

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Read captured output
	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Verify initialization logs
	if !strings.Contains(output, "JSON logging wrapper initialized") {
		t.Error("Expected initialization log message")
	}
	if !strings.Contains(output, "Stunner instance created") {
		t.Error("Expected Stunner creation log message")
	}

	// Cleanup
	wrapper.Close()
}

func TestStunnerWrapper_LoadConfiguration(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Create wrapper and setup JSON logging
	wrapper := NewStunnerWrapper()
	wrapper.SetupJSONLogging()

	// Initialize Stunner first
	options := stunner.Options{
		Name:     "test-stunner",
		LogLevel: "all:INFO",
		DryRun:   true,
	}
	wrapper.InitializeStunner(options)

	// Create a simple test configuration
	config := &stnrv1.StunnerConfig{
		ApiVersion: stnrv1.ApiVersion,
		Admin: stnrv1.AdminConfig{
			LogLevel: "all:INFO",
		},
		Auth: stnrv1.AuthConfig{
			Type: "plaintext",
			Credentials: map[string]string{
				"username": "user1",
				"password": "passwd1",
			},
		},
		Listeners: []stnrv1.ListenerConfig{{
			Name:     "default-listener",
			Protocol: "udp",
			Addr:     "127.0.0.1",
			Port:     3478,
			Routes:   []string{"allow-any"},
		}},
		Clusters: []stnrv1.ClusterConfig{{
			Name:      "allow-any",
			Endpoints: []string{"0.0.0.0/0"},
		}},
	}

	// Test reconciliation
	err := wrapper.stunner.Reconcile(config)
	if err != nil {
		t.Errorf("Failed to reconcile configuration: %v", err)
	}

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Read captured output
	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Verify configuration logs
	if !strings.Contains(output, "Stunner instance created") {
		t.Error("Expected Stunner creation log message")
	}

	// Cleanup
	wrapper.Close()
}

func TestSlogWriter_Write(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Create JSON logger
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	slogger := slog.New(handler)

	// Create slogWriter
	writer := &slogWriter{logger: slogger}

	// Write test message
	testMsg := "Test standard log message"
	_, err := writer.Write([]byte(testMsg))
	if err != nil {
		t.Errorf("Failed to write to slogWriter: %v", err)
	}

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Read captured output
	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Verify JSON output
	if !strings.Contains(output, testMsg) {
		t.Errorf("Expected JSON output to contain test message, got: %s", output)
	}

	// Try to parse as JSON
	var logEntry map[string]interface{}
	if err := json.Unmarshal([]byte(output), &logEntry); err != nil {
		t.Errorf("Expected valid JSON output, got error: %v", err)
	}
} 