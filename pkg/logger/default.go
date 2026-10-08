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
// dynamically change their own loglevels. All loggers share the single Writer, so wrapping
// Writer with a RateLimitedWriter (via NewRateLimitedLoggerFactory) applies a single shared
// token bucket across all scopes.
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

	dl := logging.NewDefaultLeveledLoggerForScope(scope, f.levelFor(scope), f.Writer)
	dl.
		WithTraceLogger(log.New(f.Writer, fmt.Sprintf("%s TRACE: ", scope), defaultFlags)).
		WithDebugLogger(log.New(f.Writer, fmt.Sprintf("%s DEBUG: ", scope), defaultFlags)).
		WithInfoLogger(log.New(f.Writer, fmt.Sprintf("%s INFO: ", scope), defaultFlags)).
		WithWarnLogger(log.New(f.Writer, fmt.Sprintf("%s WARNING: ", scope), defaultFlags)).
		WithErrorLogger(log.New(f.Writer, fmt.Sprintf("%s ERROR: ", scope), defaultFlags))

	f.loggers[scope] = dl
	return dl
}

// SetWriter sets the output writer. Only affects loggers created after this call.
func (f *LeveledLoggerFactory) SetWriter(w io.Writer) {
	f.lock.Lock()
	defer f.lock.Unlock()
	f.Writer = w
}
