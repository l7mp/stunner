package stunner

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/pion/logging"
)

// LoggerFactory defines levels by scopes and creates new LeveledLogger
type LoggerFactory struct {
	Writer          io.Writer
	DefaultLogLevel logging.LogLevel
	ScopeLevels     map[string]logging.LogLevel
}

// NewLoggerFactory sets up a scoped logger for STUNner
func NewLoggerFactory(levelSpec string) *LoggerFactory {
	logger := LoggerFactory{}
	logger.DefaultLogLevel = logging.LogLevelError
	logger.ScopeLevels = make(map[string]logging.LogLevel)
	logger.Writer = os.Stdout

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
		l, found := logLevels[strings.ToUpper(level)]
		if found == false {
			continue
		}

		if strings.ToLower(scope) == "all" {
			logger.DefaultLogLevel = l
			continue
		}

		logger.ScopeLevels[scope] = l
	}
	return &logger
}

// NewLogger returns a configured LeveledLogger for the given , argsscope
func (f *LoggerFactory) NewLogger(scope string) logging.LeveledLogger {
	logLevel := f.DefaultLogLevel
	if f.ScopeLevels != nil {
		scopeLevel, found := f.ScopeLevels[scope]

		if found {
			logLevel = scopeLevel
		}
	}

	// we move the scope/level to after the timestamp and unify the format
	flags := log.Lmicroseconds | log.Lshortfile | log.Lmsgprefix

	return logging.NewDefaultLeveledLoggerForScope(scope, logLevel, f.Writer).
		WithTraceLogger(log.New(f.Writer, fmt.Sprintf("%s TRACE: ", scope), flags)).
		WithDebugLogger(log.New(f.Writer, fmt.Sprintf("%s DEBUG: ", scope), flags)).
		WithInfoLogger(log.New(f.Writer, fmt.Sprintf("%s INFO: ", scope), flags)).
		WithWarnLogger(log.New(f.Writer, fmt.Sprintf("%s WARNING: ", scope), flags)).
		WithErrorLogger(log.New(f.Writer, fmt.Sprintf("%s ERROR: ", scope), flags))
}
