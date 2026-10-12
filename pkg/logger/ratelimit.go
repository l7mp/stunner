package logger

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"sync/atomic"

	"golang.org/x/time/rate"
)

const (
	DefaultRateLimit = rate.Limit(.25)
	DefaultBurstSize = 1
)

// NewRateLimitedLoggerFactory wraps the factory's current writer with a single shared
// rate-limiting token bucket. All log scopes and both backends share this bucket.
// Must be called before any loggers are created via NewLogger.
func NewRateLimitedLoggerFactory(logger LoggerFactory, limit rate.Limit, burst int) LoggerFactory {
	var w io.Writer
	switch f := logger.(type) {
	case *LeveledLoggerFactory:
		w = f.Writer
	case *JSONLoggerFactory:
		w = f.Writer
	default:
		panic(fmt.Sprintf("logger: NewRateLimitedLoggerFactory: unsupported factory type %T", logger))
	}
	logger.SetWriter(NewRateLimitedWriter(w, limit, burst, true))
	return logger
}

// RateLimitedWriter is a writer limited by a token bucket.
type RateLimitedWriter struct {
	io.Writer
	*RateLimiter
	// Counter counts the events suppressed since the last logged one.
	Counter       atomic.Int64
	AddSuppressed bool
}

// NewRateLimitedWriter creates a writer rate-limited by a token bucket to at most limit events
// per second with the given burst size. If addSuppressed is true, the number of events suppressed
// between logged events is appended to the output.
func NewRateLimitedWriter(writer io.Writer, limit rate.Limit, burst int, addSuppressed bool) *RateLimitedWriter {
	return &RateLimitedWriter{
		Writer:        writer,
		RateLimiter:   NewRateLimiter(limit, burst),
		AddSuppressed: addSuppressed,
	}
}

// Write fulfills io.Writer.
func (w *RateLimitedWriter) Write(p []byte) (int, error) {
	n := len(p)
	if !w.Allow() {
		w.Counter.Add(1)
		return n, nil
	}

	if suppressed := w.Counter.Swap(0); w.AddSuppressed && suppressed > 0 {
		suffix := fmt.Sprintf(" (suppressed %d log events)\n", suppressed)
		p = append(bytes.TrimRight(p, "\r\n"), suffix...)
	}
	if _, err := w.Writer.Write(p); err != nil {
		return 0, err
	}

	return n, nil
}

// RateLimiter is a token bucket that can be disabled.
type RateLimiter struct {
	*rate.Limiter
	Enabled bool
}

// NewRateLimiter creates a new enabled rate limiter.
func NewRateLimiter(r rate.Limit, b int) *RateLimiter {
	return &RateLimiter{
		Limiter: rate.NewLimiter(r, b),
		Enabled: true,
	}
}

func (l *RateLimiter) EnableRateLimiter() {
	l.Enabled = true
}

func (l *RateLimiter) DisableRateLimiter() {
	l.Enabled = false
}

func (l *RateLimiter) Allow() bool {
	if !l.Enabled {
		return true
	}
	return l.Limiter.Allow()
}

// AutoFlushWriter wraps a bufio.Writer and ensures that Flush is called after every Write
// operation.
type AutoFlushWriter struct {
	*bufio.Writer
}

// NewAutoFlushWriter creates a new AutoFlushWriter.
func NewAutoFlushWriter(w io.Writer) *AutoFlushWriter {
	return &AutoFlushWriter{
		Writer: bufio.NewWriter(w),
	}
}

// Write writes the data and immediately flushes the buffer.
func (w *AutoFlushWriter) Write(p []byte) (n int, err error) {
	n, err = w.Writer.Write(p)
	if err != nil {
		return n, err
	}

	err = w.Flush()
	return n, err
}
