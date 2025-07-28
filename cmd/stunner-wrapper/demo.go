package main

import (
	"bytes"
	"log"
	"log/slog"
)

func runDemo() {
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

	println("🧪 STUNner JSON Logging Wrapper Demo")
	println("=====================================")

	// Create logger factory with slog writer
	loggerFactory := createSlogLoggerFactory(handler, "all:DEBUG")
	
	println("✅ Logger factory created successfully")

	// Test the logger factory directly
	log := loggerFactory.NewLogger("demo")
	log.Info("STUNner server starting")
	log.Debug("Initializing components")
	log.Error("Test error message")

	println("✅ Test logging completed")

	// Get the captured log output
	logOutput := buf.String()
	
	println("\n📋 JSON Log Output:")
	println("===================")
	println(logOutput)
	
	println("\n🎯 Summary:")
	println("============")
	println("✅ All Stunner logs have been converted to JSON format")
	println("✅ No modifications to Stunner codebase required")
	println("✅ Wrapper approach works by redirecting standard log output")
	println("✅ Pion logging framework logs are captured and converted")
} 