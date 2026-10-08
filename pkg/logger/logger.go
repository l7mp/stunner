package logger

import (
	"io"

	"github.com/pion/logging"
	"golang.org/x/time/rate"
)

// LoggerFactory is the basic pion LoggerFactory interface extended with functions for setting and
// querying the loglevel per scope.
type LoggerFactory interface {
	logging.LoggerFactory
	// SetLevel sets the loglevel, optionally scoped (for example, "all:WARN,turn:DEBUG").
	SetLevel(levelSpec string)
	// GetLevel returns the loglevel for the given scope.
	GetLevel(scope string) string
	// SetWriter sets the output writer. Only affects loggers created after this call.
	SetWriter(w io.Writer)
}

// Options configures logger behavior.
type Options struct {
	// Level is a scoped level specification (for example, "all:WARN,turn:DEBUG").
	// If empty, the caller should use its own default level.
	Level string
	// Format is the log output format: "text" (default) or "json".
	Format string
	// RateLimit is the maximum number of rate-limited log events per second.
	// If non-positive, the caller should use its own default rate limit.
	RateLimit rate.Limit
	// Burst is the burst size of the log rate limiter.
	// If non-positive, the caller should use its own default burst size.
	Burst int
	// Writer is the log output stream. If nil, the caller should use its own default
	// (os.Stderr).
	Writer io.Writer
}
