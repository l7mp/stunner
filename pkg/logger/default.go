package logger

import (
	"fmt"
	"io"
	"log"
	"os"

	"github.com/pion/logging"
)

// Compile-time interface assertions.
var (
	_ LoggerFactory = (*LeveledLoggerFactory)(nil)

	// DefaultLeveledLogger is used as the concrete logger stored in the registry; verify it
	// satisfies levelSetter so the unconditional store in NewLogger is always safe.
	_ levelSetter = (*logging.DefaultLeveledLogger)(nil)
)

const defaultFlags = log.Lmicroseconds | log.Lshortfile | log.Lmsgprefix

// LeveledLoggerFactory defines levels by scopes and creates new LeveledLoggers that can
// dynamically change their own loglevels. All loggers write to the single Writer, the loggers of
// the rate-limited scopes through a token bucket of their own.
type LeveledLoggerFactory struct {
	Writer io.Writer
	scopedLevels
}

// NewLoggerFactory sets up a scoped logger for STUNner.
func NewLoggerFactory(levelSpec string) LoggerFactory {
	f := &LeveledLoggerFactory{
		Writer:       os.Stderr,
		scopedLevels: newScopedLevels(),
	}
	f.SetLevel(levelSpec)
	return f
}

// NewLogger either returns the existing LeveledLogger for the given scope or creates a new one.
func (f *LeveledLoggerFactory) NewLogger(scope string) logging.LeveledLogger {
	f.lock.Lock()
	defer f.lock.Unlock()

	if l, ok := f.loggers[scope]; ok {
		return l
	}

	w := f.writerFor(scope, f.Writer)
	dl := logging.NewDefaultLeveledLoggerForScope(scope, f.levelFor(scope), w)
	dl.
		WithTraceLogger(log.New(w, fmt.Sprintf("%s TRACE: ", scope), defaultFlags)).
		WithDebugLogger(log.New(w, fmt.Sprintf("%s DEBUG: ", scope), defaultFlags)).
		WithInfoLogger(log.New(w, fmt.Sprintf("%s INFO: ", scope), defaultFlags)).
		WithWarnLogger(log.New(w, fmt.Sprintf("%s WARNING: ", scope), defaultFlags)).
		WithErrorLogger(log.New(w, fmt.Sprintf("%s ERROR: ", scope), defaultFlags))

	f.loggers[scope] = dl
	return dl
}

// SetWriter sets the output writer. Only affects loggers created after this call.
func (f *LeveledLoggerFactory) SetWriter(w io.Writer) {
	f.lock.Lock()
	defer f.lock.Unlock()
	f.Writer = w
}
