package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/pion/logging"
	"golang.org/x/time/rate"
)

const defaultFlags = log.Lmicroseconds | log.Lshortfile | log.Lmsgprefix

var logLevels = map[string]logging.LogLevel{
	"DISABLE": logging.LogLevelDisabled,
	"ERROR":   logging.LogLevelError,
	"WARN":    logging.LogLevelWarn,
	"INFO":    logging.LogLevelInfo,
	"DEBUG":   logging.LogLevelDebug,
	"TRACE":   logging.LogLevelTrace,
}

// LoggerFactory defines levels by scopes and creates new LeveledLogger.
type LoggerFactory struct {
	Writer          io.Writer
	DefaultLogLevel logging.LogLevel
	ScopeLevels     map[string]logging.LogLevel
	Loggers         map[string]*logging.DefaultLeveledLogger
}

// NewLoggerFactory sets up a scoped logger for STUNner.
func NewLoggerFactory(levelSpec string) *LoggerFactory {
	logger := LoggerFactory{}
	logger.DefaultLogLevel = logging.LogLevelError
	logger.ScopeLevels = make(map[string]logging.LogLevel)
	logger.Writer = os.Stdout

	logger.ScopeLevels = make(map[string]logging.LogLevel)
	logger.Loggers = make(map[string]*logging.DefaultLeveledLogger)

	// resets all child loggers
	logger.SetLevel(levelSpec)

	return &logger
}

// NewLogger either returns the existing LeveledLogger (if it exists) for the given scope or creates a new one.
func (f *LoggerFactory) NewLogger(scope string) logging.LeveledLogger {
	logger, found := f.Loggers[scope]
	if found {
		return logger
	}

	logLevel := f.DefaultLogLevel
	scopeLevel, found := f.ScopeLevels[scope]
	if found {
		logLevel = scopeLevel
	}

	l := logging.NewDefaultLeveledLoggerForScope(scope, logLevel, f.Writer).
		WithTraceLogger(log.New(f.Writer, fmt.Sprintf("%s TRACE: ", scope), defaultFlags)).
		WithDebugLogger(log.New(f.Writer, fmt.Sprintf("%s DEBUG: ", scope), defaultFlags)).
		WithInfoLogger(log.New(f.Writer, fmt.Sprintf("%s INFO: ", scope), defaultFlags)).
		WithWarnLogger(log.New(f.Writer, fmt.Sprintf("%s WARNING: ", scope), defaultFlags)).
		WithErrorLogger(log.New(f.Writer, fmt.Sprintf("%s ERROR: ", scope), defaultFlags))

	f.Loggers[scope] = l

	return l
}

// NewRateLimitedLogger creates a new rate-limited logger from a factory. Every loglevel is independently rate-limited with a token bucket ofthe given period and burst size. If addSuppressed is true and then the number of suppressed events is added to the output (provided that there were suppressed events).
func (f *LoggerFactory) NewRateLimitedLogger(scope string, period time.Duration, burst int, addSuppressed bool) logging.LeveledLogger {
	logger, found := f.Loggers[scope]
	if found {
		return logger
	}

	logLevel := f.DefaultLogLevel
	scopeLevel, found := f.ScopeLevels[scope]
	if found {
		logLevel = scopeLevel
	}

	l := logging.NewDefaultLeveledLoggerForScope(scope, logLevel, f.Writer).
		WithTraceLogger(log.New(NewRateLimitedWriter(f.Writer, period, burst, addSuppressed), fmt.Sprintf("%s TRACE: ", scope), defaultFlags)).
		WithDebugLogger(log.New(NewRateLimitedWriter(f.Writer, period, burst, addSuppressed), fmt.Sprintf("%s DEBUG: ", scope), defaultFlags)).
		WithInfoLogger(log.New(NewRateLimitedWriter(f.Writer, period, burst, addSuppressed), fmt.Sprintf("%s INFO: ", scope), defaultFlags)).
		WithWarnLogger(log.New(NewRateLimitedWriter(f.Writer, period, burst, addSuppressed), fmt.Sprintf("%s WARNING: ", scope), defaultFlags)).
		WithErrorLogger(log.New(NewRateLimitedWriter(f.Writer, period, burst, addSuppressed), fmt.Sprintf("%s ERROR: ", scope), defaultFlags))

	f.Loggers[scope] = l

	return l
}

type rateLimitedWriter struct {
	io.Writer
	addSuppressed bool
	counter       int
	limiter       *rate.Limiter
}

// NewRateLimitedWriter is a writer limited by a token bucket rate-limiter to at most burst events per period. If addSuppressed is true then the number of suppressed events is appended to the output.
func NewRateLimitedWriter(writer io.Writer, period time.Duration, burst int, addSuppressed bool) io.Writer {
	return &rateLimitedWriter{
		Writer:        writer,
		addSuppressed: addSuppressed,
		counter:       0,
		limiter:       rate.NewLimiter(rate.Every(period), burst),
	}
}

func (w *rateLimitedWriter) Write(p []byte) (int, error) {
	if !w.limiter.Allow() {
		w.counter++
		return 0, nil
	}
	if w.addSuppressed && w.counter > 0 {
		s := []byte(fmt.Sprintf(" (suppressed %d log events)", w.counter))
		p = append(p, s...)
	}
	n, err := w.Writer.Write(p)
	w.counter = 0

	return n, err
}

// SetLevel sets the loglevel.
func (f *LoggerFactory) SetLevel(levelSpec string) {
	levels := strings.Split(levelSpec, ",")
	for _, s := range levels {
		scopedLevel := strings.SplitN(s, ":", 2)
		if len(scopedLevel) != 2 {
			continue
		}
		scope := scopedLevel[0]
		level := scopedLevel[1]

		// set log-level
		l, found := logLevels[strings.ToUpper(level)]
		if !found {
			continue
		}

		if strings.ToLower(scope) == "all" {
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
	}
}

// GetLevel gets the loglevel for the given scope.
func (f *LoggerFactory) GetLevel(scope string) string {
	logLevel := f.DefaultLogLevel
	scopeLevel, found := f.ScopeLevels[scope]
	if found {
		logLevel = scopeLevel
	}

	return logLevel.String()
}
