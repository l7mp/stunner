package logger

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/pion/transport/v3/test"
	"github.com/stretchr/testify/assert"
	"golang.org/x/time/rate"
)

const testScope = "dummy-scope"

var logBuffer = &bytes.Buffer{}

type loggerTestCase struct {
	name, defaultLogLevel, scopeLogLevel string
	prep                                 func(lf LoggerFactory)
	tester                               func(t *testing.T, lf LoggerFactory)
}

var loggerTests = []loggerTestCase{
	{
		name:            "default-loglevel",
		defaultLogLevel: "", // default is ERROR
		scopeLogLevel:   "",
		tester: func(t *testing.T, lf LoggerFactory) {
			level := lf.GetLevel("all")
			assert.Equal(t, "Error", level, "default scope: level")

			log := lf.NewLogger(testScope)

			level = lf.GetLevel(testScope)
			assert.Equal(t, "Error", level, "dummy scope: level")

			log.Error("dummy")
			assert.Containsf(t, logreadr(), "dummy", "ERROR for level %s", level)

			log.Warn("dummy")
			assert.Zerof(t, loglenr(), "WARN for level %s", level)

			log.Info("dummy")
			assert.Zerof(t, loglenr(), "INFO for level %s", level)

			log.Debug("dummy")
			assert.Zerof(t, loglenr(), "DEBUG for level %s", level)

			log.Trace("dummy")
			assert.Zerof(t, loglenr(), "TRACE for level %s", level)
		},
	},
	{
		name:            "default-loglevel-disable-scope",
		defaultLogLevel: "", // default is ERROR
		scopeLogLevel:   "DISABLE",
		tester: func(t *testing.T, lf LoggerFactory) {
			level := lf.GetLevel("all")
			assert.Equal(t, "Error", level, "default scope: level")

			level = lf.GetLevel(testScope)
			assert.Equal(t, "Disabled", level, "dummy scope: level")

			log := lf.NewLogger(testScope)

			log.Error("dummy")
			assert.Zerof(t, loglenr(), "ERROR for level %s", level)

			log.Warn("dummy")
			assert.Zerof(t, loglenr(), "WARN for level %s", level)

			log.Info("dummy")
			assert.Zerof(t, loglenr(), "INFO for level %s", level)

			log.Debug("dummy")
			assert.Zerof(t, loglenr(), "DEBUG for level %s", level)

			log.Trace("dummy")
			assert.Zerof(t, loglenr(), "TRACE for level %s", level)
		},
	},
	{
		name:            "default-loglevel-error-scope",
		defaultLogLevel: "", // default is ERROR
		scopeLogLevel:   "ERROR",
		tester: func(t *testing.T, lf LoggerFactory) {
			level := lf.GetLevel("all")
			assert.Equal(t, "Error", level, "default scope: level")

			level = lf.GetLevel(testScope)
			assert.Equal(t, "Error", level, "dummy scope: level")

			log := lf.NewLogger(testScope)

			log.Error("dummy")
			assert.Containsf(t, logreadr(), "dummy", "ERROR for level %s", level)

			log.Warn("dummy")
			assert.Zerof(t, loglenr(), "WARN for level %s", level)

			log.Info("dummy")
			assert.Zerof(t, loglenr(), "INFO for level %s", level)

			log.Debug("dummy")
			assert.Zerof(t, loglenr(), "DEBUG for level %s", level)

			log.Trace("dummy")
			assert.Zerof(t, loglenr(), "TRACE for level %s", level)
		},
	},
	{
		name:            "default-loglevel-warn-scope",
		defaultLogLevel: "", // default is ERROR
		scopeLogLevel:   "WARN",
		tester: func(t *testing.T, lf LoggerFactory) {
			level := lf.GetLevel("all")
			assert.Equal(t, "Error", level, "default scope: level")

			level = lf.GetLevel(testScope)
			assert.Equal(t, "Warn", level, "dummy scope: level")

			log := lf.NewLogger(testScope)

			log.Error("dummy")
			assert.Containsf(t, logreadr(), "dummy", "ERROR for level %s", level)

			log.Warn("dummy")
			assert.Containsf(t, logreadr(), "dummy", "WARN for level %s", level)

			log.Info("dummy")
			assert.Zerof(t, loglenr(), "INFO for level %s", level)

			log.Debug("dummy")
			assert.Zerof(t, loglenr(), "DEBUG for level %s", level)

			log.Trace("dummy")
			assert.Zerof(t, loglenr(), "TRACE for level %s", level)
		},
	},
	{
		name:            "default-loglevel-info-scope",
		defaultLogLevel: "", // default is ERROR
		scopeLogLevel:   "INFO",
		tester: func(t *testing.T, lf LoggerFactory) {
			level := lf.GetLevel("all")
			assert.Equal(t, "Error", level, "default scope: level")

			level = lf.GetLevel(testScope)
			assert.Equal(t, "Info", level, "dummy scope: level")

			log := lf.NewLogger(testScope)

			log.Error("dummy")
			assert.Containsf(t, logreadr(), "dummy", "ERROR for level %s", level)

			log.Warn("dummy")
			assert.Containsf(t, logreadr(), "dummy", "WARN for level %s", level)

			log.Info("dummy")
			assert.Containsf(t, logreadr(), "dummy", "INFO for level %s", level)

			log.Debug("dummy")
			assert.Zerof(t, loglenr(), "DEBUG for level %s", level)

			log.Trace("dummy")
			assert.Zerof(t, loglenr(), "TRACE for level %s", level)
		},
	},
	{
		name:            "default-loglevel-debug-scope",
		defaultLogLevel: "", // default is ERROR
		scopeLogLevel:   "DEBUG",
		tester: func(t *testing.T, lf LoggerFactory) {
			level := lf.GetLevel("all")
			assert.Equal(t, "Error", level, "default scope: level")

			level = lf.GetLevel(testScope)
			assert.Equal(t, "Debug", level, "dummy scope: level")

			log := lf.NewLogger(testScope)

			log.Error("dummy")
			assert.Containsf(t, logreadr(), "dummy", "ERROR for level %s", level)

			log.Warn("dummy")
			assert.Containsf(t, logreadr(), "dummy", "WARN for level %s", level)

			log.Info("dummy")
			assert.Containsf(t, logreadr(), "dummy", "INFO for level %s", level)

			log.Debug("dummy")
			assert.Containsf(t, logreadr(), "dummy", "DEBUG for level %s", level)

			log.Trace("dummy")
			assert.Zerof(t, loglenr(), "TRACE for level %s", level)
		},
	},
	{
		name:            "default-loglevel-trace-scope",
		defaultLogLevel: "", // default is ERROR
		scopeLogLevel:   "TRACE",
		tester: func(t *testing.T, lf LoggerFactory) {
			level := lf.GetLevel("all")
			assert.Equal(t, "Error", level, "default scope: level")

			level = lf.GetLevel(testScope)
			assert.Equal(t, "Trace", level, "dummy scope: level")

			log := lf.NewLogger(testScope)

			log.Error("dummy")
			assert.Containsf(t, logreadr(), "dummy", "ERROR for level %s", level)

			log.Warn("dummy")
			assert.Containsf(t, logreadr(), "dummy", "WARN for level %s", level)

			log.Info("dummy")
			assert.Containsf(t, logreadr(), "dummy", "INFO for level %s", level)

			log.Debug("dummy")
			assert.Containsf(t, logreadr(), "dummy", "DEBUG for level %s", level)

			log.Trace("dummy")
			assert.Containsf(t, logreadr(), "dummy", "TRACE for level %s", level)
		},
	},
	{
		name:            "override-loglevel-trace-scope",
		defaultLogLevel: "all:TRACE",
		scopeLogLevel:   "ERROR",
		tester: func(t *testing.T, lf LoggerFactory) {
			level := lf.GetLevel("all")
			assert.Equal(t, "Trace", level, "default scope: level")

			level = lf.GetLevel(testScope)
			assert.Equal(t, "Error", level, "dummy scope: level")

			log := lf.NewLogger(testScope)

			log.Error("dummy")
			assert.Containsf(t, logreadr(), "dummy", "ERROR for level %s", level)

			log.Warn("dummy")
			assert.Zerof(t, loglenr(), "WARN for level %s", level)

			log.Info("dummy")
			assert.Zerof(t, loglenr(), "INFO for level %s", level)

			log.Debug("dummy")
			assert.Zerof(t, loglenr(), "DEBUG for level %s", level)

			log.Trace("dummy")
			assert.Zerof(t, loglenr(), "TRACE for level %s", level)
		},
	},
	{
		name:            "complex-loglevel-1",
		defaultLogLevel: "all:TRACE",
		scopeLogLevel:   "TRACE",
		prep: func(lf LoggerFactory) {
			lf.SetLevel("all:TRACE,dummy-scope:ERROR")
		},
		tester: func(t *testing.T, lf LoggerFactory) {
			level := lf.GetLevel("all")
			assert.Equal(t, "Trace", level, "default scope: level")

			level = lf.GetLevel(testScope)
			assert.Equal(t, "Error", level, "dummy scope: level")

			log := lf.NewLogger(testScope)

			log.Error("dummy")
			assert.Containsf(t, logreadr(), "dummy", "ERROR for level %s", level)

			log.Warn("dummy")
			assert.Zerof(t, loglenr(), "WARN for level %s", level)

			log.Info("dummy")
			assert.Zerof(t, loglenr(), "INFO for level %s", level)

			log.Debug("dummy")
			assert.Zerof(t, loglenr(), "DEBUG for level %s", level)

			log.Trace("dummy")
			assert.Zerof(t, loglenr(), "TRACE for level %s", level)
		},
	},
	{
		name:            "complex-loglevel-2",
		defaultLogLevel: "all:TRACE",
		scopeLogLevel:   "TRACE",
		prep: func(lf LoggerFactory) {
			lf.SetLevel("dummy-scope:DEBUG,nonExistentScope:TRACE,dummy-scope:ERROR,all:error,some-other-scope:TRACE")
		},
		tester: func(t *testing.T, lf LoggerFactory) {
			level := lf.GetLevel("all")
			assert.Equal(t, "Error", level, "default scope: level")

			level = lf.GetLevel(testScope)
			assert.Equal(t, "Error", level, "dummy scope: level")

			level = lf.GetLevel("some-other-scope")
			assert.Equal(t, "Trace", level, "other scope: level")

			log := lf.NewLogger(testScope)

			log.Error("dummy")
			assert.Containsf(t, logreadr(), "dummy", "ERROR for level %s", level)

			log.Warn("dummy")
			assert.Zerof(t, loglenr(), "WARN for level %s", level)

			log.Info("dummy")
			assert.Zerof(t, loglenr(), "INFO for level %s", level)

			log.Debug("dummy")
			assert.Zerof(t, loglenr(), "DEBUG for level %s", level)

			log.Trace("dummy")
			assert.Zerof(t, loglenr(), "TRACE for level %s", level)
		},
	},
	{
		name:            "set-loglevel-for-newly-created-logger",
		defaultLogLevel: "all:TRACE",
		scopeLogLevel:   "TRACE",
		prep: func(lf LoggerFactory) {
			lf.SetLevel("all:error,new-scope:DEBUG")
		},
		tester: func(t *testing.T, lf LoggerFactory) {
			level := lf.GetLevel("all")
			assert.Equal(t, "Error", level, "default scope: level")

			level = lf.GetLevel(testScope)
			assert.Equal(t, "Error", level, "dummy scope: level")

			level = lf.GetLevel("new-scope")
			assert.Equal(t, "Debug", level, "new scope: level")

			log := lf.NewLogger("new-scope")

			level = lf.GetLevel("new-scope")
			assert.Equal(t, "Debug", level, "new scope: level")

			log.Error("dummy")
			assert.Containsf(t, logreadr(), "dummy", "ERROR for level %s", level)

			log.Warn("dummy")
			assert.Containsf(t, logreadr(), "dummy", "WARN for level %s", level)

			log.Info("dummy")
			assert.Containsf(t, logreadr(), "dummy", "INFO for level %s", level)

			log.Debug("dummy")
			assert.Containsf(t, logreadr(), "dummy", "DEBUG for level %s", level)

			log.Trace("dummy")
			assert.Zerof(t, loglenr(), "TRACE for level %s", level)
		},
	},
}

func TestLogger(t *testing.T) {
	lim := test.TimeOut(time.Second * 60)
	defer lim.Stop()

	report := test.CheckRoutines(t)
	defer report()

	for _, c := range loggerTests {
		t.Run(c.name, func(t *testing.T) {
			// t.Logf("-------------- Running test: %s -------------", c.name)

			// create
			loggerFactory := NewLoggerFactory(c.defaultLogLevel).(*LeveledLoggerFactory)
			loggerFactory.Writer = logBuffer
			logreset()

			// create logger
			_ = loggerFactory.NewLogger(testScope)
			loggerFactory.SetLevel(fmt.Sprintf("%s:%s", testScope, c.scopeLogLevel))

			// prepare
			if c.prep != nil {
				c.prep(loggerFactory)
			}

			// test
			c.tester(t, loggerFactory)
		})
	}
}

// rate-limiter tests

type rateLimiterLoggerTestCase struct {
	name, level string
	limit       rate.Limit
	burst       int
	prep        func(lf LoggerFactory)
	tester      func(t *testing.T, lf LoggerFactory)
}

var rateLimitedLoggerTests = []rateLimiterLoggerTestCase{
	{
		name:  "rate-limited-logger-default",
		limit: 1.0,
		burst: 1,
		tester: func(t *testing.T, lf LoggerFactory) {
			level := lf.GetLevel(testScope)
			assert.Equal(t, "Error", level, "other scope: level")

			log := lf.NewLogger(testScope)

			// only first call should succeed
			log.Error("dummy")
			assert.Contains(t, logreadr(), "dummy")
			log.Error("dummy")
			assert.Zerof(t, loglenr(), "suppressed")
			log.Error("dummy")
			assert.Zerof(t, loglenr(), "suppressed")
		},
	},
	{
		name:  "rate-limited-logger-burst-2",
		level: "all:INFO",
		limit: 100.0,
		burst: 2,
		tester: func(t *testing.T, lf LoggerFactory) {
			level := lf.GetLevel(testScope)
			assert.Equal(t, "Info", level, "scope: level")

			log := lf.NewLogger(testScope)

			// first call should succeed
			log.Error("dummy")
			assert.Contains(t, logreadr(), "dummy")
			log.Error("dummy")
			assert.Contains(t, logreadr(), "dummy")
			log.Info("dummy")
			assert.Zerof(t, loglenr(), "suppressed")
			log.Info("dummy")
			assert.Zerof(t, loglenr(), "suppressed")

			// wait until we get another token
			time.Sleep(25 * time.Millisecond)

			log.Error("dummy")
			assert.Contains(t, logreadr(), "dummy")
			log.Error("dummy")
			assert.Contains(t, logreadr(), "dummy")
			log.Error("dummy")
			assert.Zerof(t, loglenr(), "suppressed")
			log.Error("dummy")
			assert.Zerof(t, loglenr(), "suppressed")
		},
	},
	{
		name:  "rate-limited-logger-burst-4",
		level: "all:INFO",
		limit: 100.0,
		burst: 4,
		tester: func(t *testing.T, lf LoggerFactory) {
			level := lf.GetLevel(testScope)
			assert.Equal(t, "Info", level, "scope: level")

			log := lf.NewLogger(testScope)

			// only first 4 calls should succeed
			log.Info("dummy")
			assert.Contains(t, logreadr(), "dummy")
			log.Info("dummy")
			assert.Contains(t, logreadr(), "dummy")
			log.Info("dummy")
			assert.Contains(t, logreadr(), "dummy")
			log.Info("dummy")
			assert.Contains(t, logreadr(), "dummy")
			log.Info("dummy")
			assert.Zerof(t, loglenr(), "suppressed")
			log.Info("dummy")
			assert.Zerof(t, loglenr(), "suppressed")

			// wait until we get another token
			time.Sleep(15 * time.Millisecond)

			log.Info("dummy")
			assert.Contains(t, logread(), "dummy")
			assert.Contains(t, logreadr(), "suppressed 2 log")
			// consumed all tokens: these should be suppressed
			log.Info("dummy")
			assert.Zerof(t, loglenr(), "suppressed")
			log.Info("dummy")
			assert.Zerof(t, loglenr(), "suppressed")
			log.Info("dummy")
			assert.Zerof(t, loglenr(), "suppressed")
			log.Info("dummy")
			assert.Zerof(t, loglenr(), "suppressed")
		},
	},
	{
		name:  "rate-limited-logger-global-rate-limit",
		level: "all:INFO",
		limit: 100.0,
		burst: 1,
		tester: func(t *testing.T, lf LoggerFactory) {
			level := lf.GetLevel(testScope)
			assert.Equal(t, "Info", level, "scope: level")

			log := lf.NewLogger(testScope)

			// only first call should succeed
			log.Error("dummy")
			assert.Contains(t, logreadr(), "dummy")
			log.Error("dummy")
			assert.Zerof(t, loglenr(), "suppressed")
			log.Warn("dummy")
			assert.Zerof(t, loglenr(), "suppressed")
			log.Info("dummy")
			assert.Zerof(t, loglenr(), "suppressed")
			log.Debug("dummy")
			assert.Zerof(t, loglenr(), "suppressed")
			log.Trace("dummy")
			assert.Zerof(t, loglenr(), "suppressed")

			// wait until we get another token
			time.Sleep(15 * time.Millisecond)

			log.Error("dummy")
			assert.Contains(t, logreadr(), "dummy")
			log.Error("dummy")
			assert.Zerof(t, loglenr(), "suppressed")
			log.Warn("dummy")
			assert.Zerof(t, loglenr(), "suppressed")
			log.Info("dummy")
			assert.Zerof(t, loglenr(), "suppressed")
			log.Debug("dummy")
			assert.Zerof(t, loglenr(), "suppressed")
			log.Trace("dummy")
			assert.Zerof(t, loglenr(), "suppressed")
		},
	},
}

func TestRateLimitedLogger(t *testing.T) {
	lim := test.TimeOut(time.Second * 60)
	defer lim.Stop()

	report := test.CheckRoutines(t)
	defer report()

	for _, c := range rateLimitedLoggerTests {
		t.Run(c.name, func(t *testing.T) {
			// t.Logf("-------------- Running test: %s -------------", c.name)

			// create
			loggerFactory := NewRateLimitedLoggerFactory(NewLoggerFactory(c.level), c.limit, c.burst)
			loggerFactory.Writer = logBuffer
			logreset()

			// prepare
			if c.prep != nil {
				c.prep(loggerFactory)
			}

			// t.Logf("%#v", loggerFactory)
			// t.Logf("%#v", loggerFactory.ScopeLevels)
			// t.Logf("%#v", logger)

			// test
			c.tester(t, loggerFactory)
		})
	}
}

//nolint:golint,unused
func loglen() int {
	return logBuffer.Len()
}

func loglenr() int {
	l := logBuffer.Len()
	logBuffer.Reset()
	return l
}

//nolint:golint,unused
func logreset() {
	logBuffer.Reset()
}

func logread() string {
	return logBuffer.String()
}

func logreadr() string {
	ret := logBuffer.String()
	logBuffer.Reset()
	return ret
}
