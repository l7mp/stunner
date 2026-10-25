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
	// SetRateLimit limits the loggers of the scopes starting with one of the prefixes to limit
	// events per second at every level, with the given burst, each with a token bucket of its
	// own. Only affects loggers created after this call.
	SetRateLimit(limit rate.Limit, burst int, prefixes ...string)
}

// Options configures logger behavior.
type Options struct {
	// Level is a scoped level specification (for example, "all:WARN,turn:DEBUG").
	// If empty, the caller should use its own default level.
	Level string
	// Format is the log output format: "text" (default) or "json".
	Format string
	// RateLimit is the number of log events per second a rate-limited scope may log.
	// If non-positive, the caller should use its own default rate limit.
	RateLimit rate.Limit
	// Burst is the burst size of a rate-limited scope.
	// If non-positive, the caller should use its own default burst size.
	Burst int
	// Writer is the log output stream. If nil, the caller should use its own default
	// (os.Stderr).
	Writer io.Writer
}
