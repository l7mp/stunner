package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/pion/logging"
)

// we move the scope/level to after the timestamp and unify the format
const defaultFlags = log.Lmicroseconds | log.Lshortfile | log.Lmsgprefix

// LoggerFactory defines levels by scopes and creates new LeveledLogger
type LoggerFactory struct {
	Writer          io.Writer
	DefaultLogLevel logging.LogLevel
	ScopeLevels     map[string]logging.LogLevel
	Loggers         map[string]*logging.DefaultLeveledLogger
}

// NewLoggerFactory sets up a scoped logger for STUNner
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

// NewLogger either returns the existing LeveledLoogger (if it exists) for the given scope or creates a new one
func (f *LoggerFactory) NewLogger(scope string) logging.LeveledLogger {
	logger, found := f.Loggers[scope]
	if found {
		return logger
	}

	// create a new one
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

// NewLogger returns a configured LeveledLogger for the given , argsscope
func (f *LoggerFactory) SetLevel(levelSpec string) {
	logLevels := map[string]logging.LogLevel{
		"DISABLE": logging.LogLevelDisabled,
		"ERROR":   logging.LogLevelError,
		"WARN":    logging.LogLevelWarn,
		"INFO":    logging.LogLevelInfo,
		"DEBUG":   logging.LogLevelDebug,
		"TRACE":   logging.LogLevelTrace,
	}

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
