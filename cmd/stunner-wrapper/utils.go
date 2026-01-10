package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/l7mp/stunner/pkg/logger"
)

// slogWriter bridges slog and the standard log package
type slogWriter struct {
	handler slog.Handler
	level   slog.Level
}

func (w *slogWriter) Write(p []byte) (n int, err error) {
	// Create a record with the message
	record := slog.NewRecord(time.Now(), w.level, string(p), 0)
	
	// Handle the record
	w.handler.Handle(context.Background(), record)
	
	return len(p), nil
}

// setupSlogRedirect configures slog and redirects standard log to it
func setupSlogRedirect(handler slog.Handler) *slogWriter {
	// Create slog logger
	slogger := slog.New(handler)
	slog.SetDefault(slogger)

	// Create a writer that bridges slog and the standard log package
	logWriter := &slogWriter{
		handler: handler,
		level:   slog.LevelInfo,
	}
	
	return logWriter
}

// createSlogLoggerFactory creates a Stunner logger factory that uses slog
func createSlogLoggerFactory(handler slog.Handler, levelSpec string) logger.LoggerFactory {
	logWriter := &slogWriter{
		handler: handler,
		level:   slog.LevelInfo,
	}
	
	// Create the logger factory with our slog writer
	lf := logger.NewLoggerFactory(levelSpec)
	lf.SetWriter(logWriter)
	
	return lf
} 