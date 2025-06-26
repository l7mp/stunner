package logger

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/pion/logging"
	"golang.org/x/time/rate"
)

const (
	defaultFlags     = log.Lmicroseconds | log.Lshortfile | log.Lmsgprefix
	DefaultRateLimit = rate.Limit(.25)
	DefaultBurstSize = 1
)

var logLevels = map[string]logging.LogLevel{
	"DISABLE": logging.LogLevelDisabled,
	"ERROR":   logging.LogLevelError,
	"WARN":    logging.LogLevelWarn,
	"INFO":    logging.LogLevelInfo,
	"DEBUG":   logging.LogLevelDebug,
	"TRACE":   logging.LogLevelTrace,
}

// LoggerFactory is the basic pion LoggerFactory interface extended with functions for setting and querying the loglevel per scope.
type LoggerFactory interface {
	logging.LoggerFactory
	// SetLevel sets the global loglevel.
	SetLevel(levelSpec string)
	// GetLevel gets the loglevel for the given scope.
	GetLevel(scope string) string
	// SetWriter decorates a logger factory with a writer.
	SetWriter(w io.Writer)
}

// LeveledLoggerFactory defines levels by scopes and creates new LeveledLoggers that can dynamically change their own loglevels.
type LeveledLoggerFactory struct {
	Writer          io.Writer
	DefaultLogLevel logging.LogLevel
	ScopeLevels     map[string]logging.LogLevel
	Loggers         map[string]*RateLimitedLogger
	lock            sync.RWMutex
}

// NewLoggerFactory sets up a scoped logger for STUNner.
func NewLoggerFactory(levelSpec string) LoggerFactory {
	logger := LeveledLoggerFactory{}
	logger.DefaultLogLevel = logging.LogLevelError
	logger.ScopeLevels = make(map[string]logging.LogLevel)
	logger.Loggers = make(map[string]*RateLimitedLogger)

	// Set writer
	logger.SetWriter(os.Stdout)

	// resets all child loggers
	logger.SetLevel(levelSpec)

	return &logger
}

// NewLogger either returns the existing LeveledLogger (if it exists) for the given scope or creates a new one.
func (f *LeveledLoggerFactory) NewLogger(scope string) logging.LeveledLogger {
	logger := f.newLogger(scope, DefaultRateLimit, DefaultBurstSize)
	logger.DisableRateLimiter()
	return logger
}

// SetWriter sets the writer underlying the logger.
func (f *LeveledLoggerFactory) SetWriter(w io.Writer) {
	f.Writer = w
}

// SetLevel sets the loglevel.
func (f *LeveledLoggerFactory) SetLevel(levelSpec string) {
	f.lock.Lock()
	defer f.lock.Unlock()

	levels := strings.Split(levelSpec, ",")
	for _, s := range levels {
		scopedLevel := strings.SplitN(s, ":", 2)
		if len(scopedLevel) != 2 {
			continue
		}
		scope := scopedLevel[0]
		level := scopedLevel[1]

		l, ok := logLevels[strings.ToUpper(level)]
		if !ok {
			continue
		}

		if strings.ToLower(scope) == "all" {
			for c := range f.Loggers {
				f.ScopeLevels[c] = l
			}
			f.DefaultLogLevel = l
			continue
		}
		f.ScopeLevels[scope] = l
	}

	for scope, logger := range f.Loggers {
		l, found := f.ScopeLevels[scope]
		if !found {
			l = f.DefaultLogLevel
		}

		logger.SetLevel(l)

		// disable rate-limiting at DEBUG and TRACE level
		if l == logging.LogLevelDebug || l == logging.LogLevelTrace {
			logger.DisableRateLimiter()
		}
	}
}

// GetLevel gets the loglevel for the given scope.
func (f *LeveledLoggerFactory) GetLevel(scope string) string {
	f.lock.RLock()
	defer f.lock.RUnlock()

	logLevel := f.DefaultLogLevel
	scopeLevel, found := f.ScopeLevels[scope]
	if found {
		logLevel = scopeLevel
	}

	return logLevel.String()
}

// RateLimitedLoggerFactory is a logger factory that can emit rate-limited loggers. Note that all
// loglevels are rate-limited via single token bucket. Rate-limiting only applies at high loglevels
// (ERROR, WARN and INFO), a logger set to alower loglevel (DEBUG and TRACE) is never rate-limited
// to ease debugging.
type RateLimitedLoggerFactory struct {
	*LeveledLoggerFactory
	Limit rate.Limit
	Burst int
}

// WithRateLimiter decorates a logger factory with a rate-limiter. All loggers emitted by the
// factory will be automatically rate-limited.
func NewRateLimitedLoggerFactory(logger LoggerFactory, limit rate.Limit, burst int) *RateLimitedLoggerFactory {
	leveledF, ok := logger.(*LeveledLoggerFactory)
	if !ok {
		return nil // this will blow up
	}
	return &RateLimitedLoggerFactory{
		LeveledLoggerFactory: leveledF,
		Limit:                limit,
		Burst:                burst,
	}
}

// NewLogger either returns the existing LeveledLogger (if it exists) for the given scope or creates a new one.
func (f *RateLimitedLoggerFactory) NewLogger(scope string) logging.LeveledLogger {
	logger := f.newLogger(scope, f.Limit, f.Burst)

	// disable rate-limiting logging at lower loglevels
	l := f.DefaultLogLevel

	scopeLevel, found := f.ScopeLevels[scope]
	if found {
		l = scopeLevel
	}

	// disable rate-limiting at DEBUG and TRACE level
	if l == logging.LogLevelDebug || l == logging.LogLevelTrace {
		logger.DisableRateLimiter()
	} else {
		logger.EnableRateLimiter()
	}

	return logger
}

// RateLimitedLogger is a rate-limiter logger for a specific scope.
type RateLimitedLogger struct {
	*logging.DefaultLeveledLogger
	*RateLimitedWriter
}

// NewRateLimitedLoggerForScope returns a LeveledLogger configured with a default rate limiter.
func NewRateLimitedLoggerForScope(scope string, level logging.LogLevel, writer io.Writer, limit rate.Limit, burst int) *RateLimitedLogger {
	// NewLogger will set the limit and burst
	w := NewRateLimitedWriter(writer, limit, burst, true)
	return &RateLimitedLogger{
		DefaultLeveledLogger: logging.NewDefaultLeveledLoggerForScope(scope, level, writer),
		RateLimitedWriter:    w,
	}
}

// newLogger knows how to emit rate-limited loggers.
func (f *LeveledLoggerFactory) newLogger(scope string, limit rate.Limit, burst int) *RateLimitedLogger {
	f.lock.Lock()
	defer f.lock.Unlock()

	logger, found := f.Loggers[scope]
	if found {
		return logger
	}

	logLevel := f.DefaultLogLevel
	scopeLevel, found := f.ScopeLevels[scope]
	if found {
		logLevel = scopeLevel
	}

	l := NewRateLimitedLoggerForScope(scope, logLevel, f.Writer, limit, burst)

	l.DefaultLeveledLogger.
		WithTraceLogger(log.New(l.RateLimitedWriter, fmt.Sprintf("%s TRACE: ", scope), defaultFlags)).
		WithDebugLogger(log.New(l.RateLimitedWriter, fmt.Sprintf("%s DEBUG: ", scope), defaultFlags)).
		WithInfoLogger(log.New(l.RateLimitedWriter, fmt.Sprintf("%s INFO: ", scope), defaultFlags)).
		WithWarnLogger(log.New(l.RateLimitedWriter, fmt.Sprintf("%s WARNING: ", scope), defaultFlags)).
		WithErrorLogger(log.New(l.RateLimitedWriter, fmt.Sprintf("%s ERROR: ", scope), defaultFlags))

	f.Loggers[scope] = l

	return l
}

// RateLimitedWriter is a writer limited by a token bucket.
type RateLimitedWriter struct {
	io.Writer
	*RateLimiter
	Counter       int
	AddSuppressed bool
}

// NewRateLimitedWriter creates a writer rate-limited by a token bucket to at most limit events per
// second with the given burst size. If addSuppressed is true then the number of events suppressed
// between logged events is appended to the output.
func NewRateLimitedWriter(writer io.Writer, limit rate.Limit, burst int, addSuppressed bool) *RateLimitedWriter {
	return &RateLimitedWriter{
		Writer:        writer,
		RateLimiter:   NewRateLimiter(limit, burst),
		Counter:       0, // no need to lock: we are being called under a lock from DefaultLeveledLogger
		AddSuppressed: addSuppressed,
	}
}

// Write fulfills io.Writer.
func (w *RateLimitedWriter) Write(p []byte) (int, error) {
	if !w.Allow() {
		w.Counter++
		return 0, nil
	}

	if w.AddSuppressed && w.Counter > 0 {
		suffix := fmt.Sprintf(" (suppressed %d log events)\n", w.Counter)
		p = append(bytes.TrimRight(p, "\r\n"), suffix...)
	}
	n, err := w.Writer.Write(p)
	w.Counter = 0

	return n, err
}

// RateLimiter is a token bucket that can be disabled.
type RateLimiter struct {
	*rate.Limiter
	Enabled bool
}

func NewRateLimiter(r rate.Limit, b int) *RateLimiter {
	return &RateLimiter{
		Limiter: rate.NewLimiter(r, b),
		Enabled: false,
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

// Write writes the data and immediately flushes the buffer
func (w *AutoFlushWriter) Write(p []byte) (n int, err error) {
	n, err = w.Writer.Write(p)
	if err != nil {
		return n, err
	}

	err = w.Flush()
	return n, err
}
