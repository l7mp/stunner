package logger

import (
	"io"
	"os"

	"github.com/pion/logging"
)

// Compile-time interface assertion.
var _ LoggerFactory = (*JSONLoggerFactory)(nil)

// JSONLoggerFactory wraps pion's JSON logger factory and satisfies STUNner's LoggerFactory
// interface (which adds SetLevel, GetLevel, and SetWriter on top of pion's NewLogger).
// Like LeveledLoggerFactory, all scopes share the single Writer.
type JSONLoggerFactory struct {
	Writer io.Writer
	scopedLevels
}

// NewJSONLoggerFactory creates a LoggerFactory that emits structured JSON log lines via
// pion/logging's slog-based JSON backend.
func NewJSONLoggerFactory(levelSpec string) LoggerFactory {
	f := &JSONLoggerFactory{
		Writer:       os.Stderr,
		scopedLevels: newScopedLevels(),
	}
	f.SetLevel(levelSpec)
	return f
}

// NewLogger returns (or creates) the JSON leveled logger for the given scope.
// The assertion l.(levelSetter) enforces that pion's JSON logger satisfies the registry
// contract; it panics at the first NewLogger call if the pion API ever changes.
func (f *JSONLoggerFactory) NewLogger(scope string) logging.LeveledLogger {
	f.lock.Lock()
	defer f.lock.Unlock()

	if l, ok := f.loggers[scope]; ok {
		return l
	}

	l := logging.NewJSONLoggerFactory(
		logging.WithJSONWriter(f.Writer),
		logging.WithJSONDefaultLevel(f.levelFor(scope)),
	).NewLogger(scope)

	f.loggers[scope] = l.(levelSetter)
	return l
}

// SetWriter sets the output writer. Only affects loggers created after this call.
func (f *JSONLoggerFactory) SetWriter(w io.Writer) {
	f.lock.Lock()
	defer f.lock.Unlock()
	f.Writer = w
}
