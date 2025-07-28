package main

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

// LoggerInterceptor captures and converts all log output to JSON
type LoggerInterceptor struct {
	jsonLogger *slog.Logger
	originalStdout *os.File
	originalStderr *os.File
	stdoutPipe     *os.File
	stderrPipe     *os.File
	wg             sync.WaitGroup
	stopChan       chan struct{}
}

// NewLoggerInterceptor creates a new logger interceptor
func NewLoggerInterceptor(jsonLogger *slog.Logger) *LoggerInterceptor {
	return &LoggerInterceptor{
		jsonLogger: jsonLogger,
		stopChan:   make(chan struct{}),
	}
}

// Start begins intercepting all log output
func (li *LoggerInterceptor) Start() error {
	// Save original stdout/stderr
	li.originalStdout = os.Stdout
	li.originalStderr = os.Stderr

	// Create pipes for stdout and stderr
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		return err
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		return err
	}

	li.stdoutPipe = stdoutW
	li.stderrPipe = stderrW

	// Redirect stdout and stderr to our pipes
	os.Stdout = stdoutW
	os.Stderr = stderrW

	// Start goroutines to capture and convert output
	li.wg.Add(2)
	go li.captureOutput(stdoutR, "stdout")
	go li.captureOutput(stderrR, "stderr")

	return nil
}

// Stop stops the interceptor and restores original output
func (li *LoggerInterceptor) Stop() {
	close(li.stopChan)
	
	// Restore original stdout/stderr
	os.Stdout = li.originalStdout
	os.Stderr = li.originalStderr
	
	// Close pipes
	if li.stdoutPipe != nil {
		li.stdoutPipe.Close()
	}
	if li.stderrPipe != nil {
		li.stderrPipe.Close()
	}
	
	li.wg.Wait()
}

// captureOutput captures output from a pipe and converts it to JSON
func (li *LoggerInterceptor) captureOutput(r *os.File, source string) {
	defer li.wg.Done()
	defer r.Close()

	buf := make([]byte, 1024)
	for {
		select {
		case <-li.stopChan:
			return
		default:
			n, err := r.Read(buf)
			if err != nil {
				if err != io.EOF {
					li.jsonLogger.Error("Error reading from pipe", "source", source, "error", err.Error())
				}
				return
			}
			if n > 0 {
				// Split by lines and process each line
				lines := strings.Split(string(buf[:n]), "\n")
				for _, line := range lines {
					line = strings.TrimSpace(line)
					if line != "" {
						// Try to parse as structured log first
						if li.tryParseStructuredLog(line, source) {
							continue
						}
						
						// Fall back to simple message logging
						li.jsonLogger.Info("log_message", 
							"source", source,
							"message", line)
					}
				}
			}
		}
	}
}

// tryParseStructuredLog attempts to parse a log line as structured data
func (li *LoggerInterceptor) tryParseStructuredLog(line, source string) bool {
	// Look for common log patterns and extract structured data
	if strings.Contains(line, "DEBUG:") {
		parts := strings.SplitN(line, "DEBUG:", 2)
		if len(parts) == 2 {
			li.jsonLogger.Debug("debug_message",
				"source", source,
				"component", strings.TrimSpace(parts[0]),
				"message", strings.TrimSpace(parts[1]))
			return true
		}
	}
	
	if strings.Contains(line, "INFO:") {
		parts := strings.SplitN(line, "INFO:", 2)
		if len(parts) == 2 {
			li.jsonLogger.Info("info_message",
				"source", source,
				"component", strings.TrimSpace(parts[0]),
				"message", strings.TrimSpace(parts[1]))
			return true
		}
	}
	
	if strings.Contains(line, "ERROR:") {
		parts := strings.SplitN(line, "ERROR:", 2)
		if len(parts) == 2 {
			li.jsonLogger.Error("error_message",
				"source", source,
				"component", strings.TrimSpace(parts[0]),
				"message", strings.TrimSpace(parts[1]))
			return true
		}
	}
	
	if strings.Contains(line, "WARN:") {
		parts := strings.SplitN(line, "WARN:", 2)
		if len(parts) == 2 {
			li.jsonLogger.Warn("warn_message",
				"source", source,
				"component", strings.TrimSpace(parts[0]),
				"message", strings.TrimSpace(parts[1]))
			return true
		}
	}
	
	return false
}

// Write implements io.Writer to capture any direct writes
func (li *LoggerInterceptor) Write(p []byte) (n int, err error) {
	msg := strings.TrimSpace(string(p))
	if msg != "" {
		li.jsonLogger.Info("direct_write", "message", msg)
	}
	return len(p), nil
} 