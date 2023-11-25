package logger

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/pion/transport/v3/test"
	"github.com/stretchr/testify/assert"
)

const testScope = "dummy-scope"

type loggerTestCase struct {
	name, defaultLogLevel, scopeLogLevel string
	prep                                 func(lf *LoggerFactory)
	tester                               func(t *testing.T, lf *LoggerFactory)
}

var loggerTests = []loggerTestCase{
	{
		name:            "default-loglevel",
		defaultLogLevel: "", // default is ERROR
		scopeLogLevel:   "",
		tester: func(t *testing.T, lf *LoggerFactory) {
			level := lf.GetLevel("all")
			assert.Equal(t, "Error", level, "default scope: level")

			level = lf.GetLevel(testScope)
			assert.Equal(t, "Error", level, "dummy scope: level")

			// resuse logger
			ld := lf.NewLogger(testScope)

			ld.Error("dummy")
			assert.Containsf(t, lf.readr(), "dummy", "ERROR for level %s", level)

			ld.Warn("dummy")
			assert.Zerof(t, lf.lenr(), "WARN for level %s", level)

			ld.Info("dummy")
			assert.Zerof(t, lf.lenr(), "INFO for level %s", level)

			ld.Debug("dummy")
			assert.Zerof(t, lf.lenr(), "DEBUG for level %s", level)

			ld.Trace("dummy")
			assert.Zerof(t, lf.lenr(), "TRACE for level %s", level)
		},
	},
	{
		name:            "default-loglevel-disable-scope",
		defaultLogLevel: "", // default is ERROR
		scopeLogLevel:   "DISABLE",
		tester: func(t *testing.T, lf *LoggerFactory) {
			level := lf.GetLevel("all")
			assert.Equal(t, "Error", level, "default scope: level")

			level = lf.GetLevel(testScope)
			assert.Equal(t, "Disabled", level, "dummy scope: level")

			// reuse logger
			ld := lf.NewLogger(testScope)

			ld.Error("dummy")
			assert.Zerof(t, lf.lenr(), "ERROR for level %s", level)

			ld.Warn("dummy")
			assert.Zerof(t, lf.lenr(), "WARN for level %s", level)

			ld.Info("dummy")
			assert.Zerof(t, lf.lenr(), "INFO for level %s", level)

			ld.Debug("dummy")
			assert.Zerof(t, lf.lenr(), "DEBUG for level %s", level)

			ld.Trace("dummy")
			assert.Zerof(t, lf.lenr(), "TRACE for level %s", level)
		},
	},
	{
		name:            "default-loglevel-error-scope",
		defaultLogLevel: "", // default is ERROR
		scopeLogLevel:   "ERROR",
		tester: func(t *testing.T, lf *LoggerFactory) {
			level := lf.GetLevel("all")
			assert.Equal(t, "Error", level, "default scope: level")

			level = lf.GetLevel(testScope)
			assert.Equal(t, "Error", level, "dummy scope: level")

			// reuse logger
			ld := lf.NewLogger(testScope)

			ld.Error("dummy")
			assert.Containsf(t, lf.readr(), "dummy", "ERROR for level %s", level)

			ld.Warn("dummy")
			assert.Zerof(t, lf.lenr(), "WARN for level %s", level)

			ld.Info("dummy")
			assert.Zerof(t, lf.lenr(), "INFO for level %s", level)

			ld.Debug("dummy")
			assert.Zerof(t, lf.lenr(), "DEBUG for level %s", level)

			ld.Trace("dummy")
			assert.Zerof(t, lf.lenr(), "TRACE for level %s", level)
		},
	},
	{
		name:            "default-loglevel-warn-scope",
		defaultLogLevel: "", // default is ERROR
		scopeLogLevel:   "WARN",
		tester: func(t *testing.T, lf *LoggerFactory) {
			level := lf.GetLevel("all")
			assert.Equal(t, "Error", level, "default scope: level")

			level = lf.GetLevel(testScope)
			assert.Equal(t, "Warn", level, "dummy scope: level")

			// reuse logger
			ld := lf.NewLogger(testScope)

			ld.Error("dummy")
			assert.Containsf(t, lf.readr(), "dummy", "ERROR for level %s", level)

			ld.Warn("dummy")
			assert.Containsf(t, lf.readr(), "dummy", "WARN for level %s", level)

			ld.Info("dummy")
			assert.Zerof(t, lf.lenr(), "INFO for level %s", level)

			ld.Debug("dummy")
			assert.Zerof(t, lf.lenr(), "DEBUG for level %s", level)

			ld.Trace("dummy")
			assert.Zerof(t, lf.lenr(), "TRACE for level %s", level)
		},
	},
	{
		name:            "default-loglevel-info-scope",
		defaultLogLevel: "", // default is ERROR
		scopeLogLevel:   "INFO",
		tester: func(t *testing.T, lf *LoggerFactory) {
			level := lf.GetLevel("all")
			assert.Equal(t, "Error", level, "default scope: level")

			level = lf.GetLevel(testScope)
			assert.Equal(t, "Info", level, "dummy scope: level")

			// reuse logger
			ld := lf.NewLogger(testScope)

			ld.Error("dummy")
			assert.Containsf(t, lf.readr(), "dummy", "ERROR for level %s", level)

			ld.Warn("dummy")
			assert.Containsf(t, lf.readr(), "dummy", "WARN for level %s", level)

			ld.Info("dummy")
			assert.Containsf(t, lf.readr(), "dummy", "INFO for level %s", level)

			ld.Debug("dummy")
			assert.Zerof(t, lf.lenr(), "DEBUG for level %s", level)

			ld.Trace("dummy")
			assert.Zerof(t, lf.lenr(), "TRACE for level %s", level)
		},
	},
	{
		name:            "default-loglevel-debug-scope",
		defaultLogLevel: "", // default is ERROR
		scopeLogLevel:   "DEBUG",
		tester: func(t *testing.T, lf *LoggerFactory) {
			level := lf.GetLevel("all")
			assert.Equal(t, "Error", level, "default scope: level")

			level = lf.GetLevel(testScope)
			assert.Equal(t, "Debug", level, "dummy scope: level")

			// reuse logger
			ld := lf.NewLogger(testScope)

			ld.Error("dummy")
			assert.Containsf(t, lf.readr(), "dummy", "ERROR for level %s", level)

			ld.Warn("dummy")
			assert.Containsf(t, lf.readr(), "dummy", "WARN for level %s", level)

			ld.Info("dummy")
			assert.Containsf(t, lf.readr(), "dummy", "INFO for level %s", level)

			ld.Debug("dummy")
			assert.Containsf(t, lf.readr(), "dummy", "DEBUG for level %s", level)

			ld.Trace("dummy")
			assert.Zerof(t, lf.lenr(), "TRACE for level %s", level)
		},
	},
	{
		name:            "default-loglevel-trace-scope",
		defaultLogLevel: "", // default is ERROR
		scopeLogLevel:   "TRACE",
		tester: func(t *testing.T, lf *LoggerFactory) {
			level := lf.GetLevel("all")
			assert.Equal(t, "Error", level, "default scope: level")

			level = lf.GetLevel(testScope)
			assert.Equal(t, "Trace", level, "dummy scope: level")

			// reuse logger
			ld := lf.NewLogger(testScope)

			ld.Error("dummy")
			assert.Containsf(t, lf.readr(), "dummy", "ERROR for level %s", level)

			ld.Warn("dummy")
			assert.Containsf(t, lf.readr(), "dummy", "WARN for level %s", level)

			ld.Info("dummy")
			assert.Containsf(t, lf.readr(), "dummy", "INFO for level %s", level)

			ld.Debug("dummy")
			assert.Containsf(t, lf.readr(), "dummy", "DEBUG for level %s", level)

			ld.Trace("dummy")
			assert.Containsf(t, lf.readr(), "dummy", "TRACE for level %s", level)
		},
	},
	{
		name:            "override-loglevel-trace-scope",
		defaultLogLevel: "all:TRACE",
		scopeLogLevel:   "ERROR",
		tester: func(t *testing.T, lf *LoggerFactory) {
			level := lf.GetLevel("all")
			assert.Equal(t, "Trace", level, "default scope: level")

			level = lf.GetLevel(testScope)
			assert.Equal(t, "Error", level, "dummy scope: level")

			// reuse logger
			ld := lf.NewLogger(testScope)

			ld.Error("dummy")
			assert.Containsf(t, lf.readr(), "dummy", "ERROR for level %s", level)

			ld.Warn("dummy")
			assert.Zerof(t, lf.lenr(), "WARN for level %s", level)

			ld.Info("dummy")
			assert.Zerof(t, lf.lenr(), "INFO for level %s", level)

			ld.Debug("dummy")
			assert.Zerof(t, lf.lenr(), "DEBUG for level %s", level)

			ld.Trace("dummy")
			assert.Zerof(t, lf.lenr(), "TRACE for level %s", level)
		},
	},
	{
		name:            "complex-loglevel-1",
		defaultLogLevel: "all:TRACE",
		scopeLogLevel:   "TRACE",
		prep: func(lf *LoggerFactory) {
			lf.SetLevel("all:TRACE,dummy-scope:ERROR")
		},
		tester: func(t *testing.T, lf *LoggerFactory) {
			level := lf.GetLevel("all")
			assert.Equal(t, "Trace", level, "default scope: level")

			level = lf.GetLevel(testScope)
			assert.Equal(t, "Error", level, "dummy scope: level")

			// reuse logger
			ld := lf.NewLogger(testScope)

			ld.Error("dummy")
			assert.Containsf(t, lf.readr(), "dummy", "ERROR for level %s", level)

			ld.Warn("dummy")
			assert.Zerof(t, lf.lenr(), "WARN for level %s", level)

			ld.Info("dummy")
			assert.Zerof(t, lf.lenr(), "INFO for level %s", level)

			ld.Debug("dummy")
			assert.Zerof(t, lf.lenr(), "DEBUG for level %s", level)

			ld.Trace("dummy")
			assert.Zerof(t, lf.lenr(), "TRACE for level %s", level)
		},
	},
	{
		name:            "complex-loglevel-2",
		defaultLogLevel: "all:TRACE",
		scopeLogLevel:   "TRACE",
		prep: func(lf *LoggerFactory) {
			lf.SetLevel("dummy-scope:DEBUG,nonExistentScope:TRACE,dummy-scope:ERROR,all:TRACE")
		},
		tester: func(t *testing.T, lf *LoggerFactory) {
			level := lf.GetLevel("all")
			assert.Equal(t, "Trace", level, "default scope: level")

			level = lf.GetLevel(testScope)
			assert.Equal(t, "Error", level, "dummy scope: level")

			// reuse logger
			ld := lf.NewLogger(testScope)

			ld.Error("dummy")
			assert.Containsf(t, lf.readr(), "dummy", "ERROR for level %s", level)

			ld.Warn("dummy")
			assert.Zerof(t, lf.lenr(), "WARN for level %s", level)

			ld.Info("dummy")
			assert.Zerof(t, lf.lenr(), "INFO for level %s", level)

			ld.Debug("dummy")
			assert.Zerof(t, lf.lenr(), "DEBUG for level %s", level)

			ld.Trace("dummy")
			assert.Zerof(t, lf.lenr(), "TRACE for level %s", level)
		},
	},
}

func (lf *LoggerFactory) len() int {
	b, ok := lf.Writer.(*bytes.Buffer)
	if !ok {
		panic("not a test logger factory")
	}
	return b.Len()
}

func (lf *LoggerFactory) lenr() int {
	b, ok := lf.Writer.(*bytes.Buffer)
	if !ok {
		panic("not a test logger factory")
	}
	l := b.Len()
	b.Reset()
	return l
}

func (lf *LoggerFactory) reset() {
	b, ok := lf.Writer.(*bytes.Buffer)
	if !ok {
		panic("not a test logger factory")
	}
	b.Reset()
}

func (lf *LoggerFactory) read() string {
	b, ok := lf.Writer.(*bytes.Buffer)
	if !ok {
		panic("not a test logger factory")
	}
	return b.String()
}

func (lf *LoggerFactory) readr() string {
	b, ok := lf.Writer.(*bytes.Buffer)
	if !ok {
		panic("not a test logger factory")
	}
	ret := b.String()
	b.Reset()
	return ret
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
			loggerFactory := NewLoggerFactory(c.defaultLogLevel)
			loggerFactory.Writer = &bytes.Buffer{}

			// create logger
			_ = loggerFactory.NewLogger(testScope)
			loggerFactory.SetLevel(fmt.Sprintf("%s:%s", testScope, c.scopeLogLevel))

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

// rate-limiter tests

type rateLimiterLoggerTestCase struct {
	name          string
	addSuppressed bool
	period        time.Duration
	burst         int
	prep          func(lf *LoggerFactory)
	tester        func(t *testing.T, lf *LoggerFactory)
}

var rateLimitedLoggerTests = []rateLimiterLoggerTestCase{
	{
		name:          "rate-limited-logger-default",
		addSuppressed: true,
		period:        10 * time.Millisecond,
		burst:         1,
		tester: func(t *testing.T, lf *LoggerFactory) {
			// reuse logger
			ld := lf.NewLogger(testScope)

			// only first call should succeed
			ld.Info("dummy")
			assert.Contains(t, lf.readr(), "dummy")
			ld.Info("dummy")
			assert.Zerof(t, lf.lenr(), "suppressed")
			ld.Info("dummy")
			assert.Zerof(t, lf.lenr(), "suppressed")

			// wait until we get another token
			time.Sleep(15 * time.Millisecond)

			ld.Info("dummy")
			assert.Contains(t, lf.read(), "dummy")
			assert.Contains(t, lf.readr(), "suppressed 2 log")
			ld.Info("dummy")
			assert.Zerof(t, lf.lenr(), "suppressed")
			ld.Info("dummy")
			assert.Zerof(t, lf.lenr(), "suppressed")
		},
	},
	{
		name:          "rate-limited-logger-suppressed-shown",
		addSuppressed: true,
		period:        10 * time.Millisecond,
		burst:         1,
		tester: func(t *testing.T, lf *LoggerFactory) {
			// reuse logger
			ld := lf.NewLogger(testScope)

			// first call should succeed
			ld.Info("dummy")
			assert.Contains(t, lf.readr(), "dummy")

			// wait until we get another token
			time.Sleep(15 * time.Millisecond)

			ld.Info("dummy")
			assert.Contains(t, lf.readr(), "dummy")
			// no "suppressed" message should appear
			assert.NotContains(t, lf.readr(), "suppressed 2 log")

			ld.Info("dummy")
			assert.Zerof(t, lf.lenr(), "suppressed")
			ld.Info("dummy")
			assert.Zerof(t, lf.lenr(), "suppressed")
		},
	},
	{
		name:          "rate-limited-logger-suppressed-supressed",
		addSuppressed: false,
		period:        10 * time.Millisecond,
		burst:         1,
		tester: func(t *testing.T, lf *LoggerFactory) {
			// reuse logger
			ld := lf.NewLogger(testScope)

			// only first call should succeed
			ld.Info("dummy")
			assert.Contains(t, lf.readr(), "dummy")
			ld.Info("dummy")
			assert.Zerof(t, lf.lenr(), "suppressed")
			ld.Info("dummy")
			assert.Zerof(t, lf.lenr(), "suppressed")

			// wait until we get another token
			time.Sleep(15 * time.Millisecond)

			ld.Info("dummy")
			assert.Contains(t, lf.read(), "dummy")
			assert.NotContains(t, lf.readr(), "suppressed 2 log")
			ld.Info("dummy")
			assert.Zerof(t, lf.lenr(), "suppressed")
			ld.Info("dummy")
			assert.Zerof(t, lf.lenr(), "suppressed")
		},
	},
	{
		name:          "rate-limited-logger-burst-3",
		addSuppressed: true,
		period:        10 * time.Millisecond,
		burst:         3,
		tester: func(t *testing.T, lf *LoggerFactory) {
			// reuse logger
			ld := lf.NewLogger(testScope)

			// only first 3 calls should succeed
			ld.Info("dummy")
			assert.Contains(t, lf.readr(), "dummy")
			ld.Info("dummy")
			assert.Contains(t, lf.readr(), "dummy")
			ld.Info("dummy")
			assert.Contains(t, lf.readr(), "dummy")
			ld.Info("dummy")
			assert.Zerof(t, lf.lenr(), "suppressed")
			ld.Info("dummy")
			assert.Zerof(t, lf.lenr(), "suppressed")

			// wait until we get another token
			time.Sleep(13 * time.Millisecond)

			ld.Info("dummy")
			assert.Contains(t, lf.read(), "dummy")
			assert.Contains(t, lf.readr(), "suppressed 2 log")
			// consumed all tokens: these should be suppressed
			ld.Info("dummy")
			assert.Zerof(t, lf.lenr(), "suppressed")
			ld.Info("dummy")
			assert.Zerof(t, lf.lenr(), "suppressed")
			ld.Info("dummy")
			assert.Zerof(t, lf.lenr(), "suppressed")
			ld.Info("dummy")
			assert.Zerof(t, lf.lenr(), "suppressed")
		},
	},
	{
		name:          "rate-limited-logger-independent",
		addSuppressed: true,
		period:        10 * time.Millisecond,
		burst:         1,
		tester: func(t *testing.T, lf *LoggerFactory) {
			// reuse logger
			ld := lf.NewLogger(testScope)

			// only first call should succeed
			ld.Info("dummy")
			assert.Contains(t, lf.readr(), "dummy")
			ld.Info("dummy")
			assert.Zerof(t, lf.lenr(), "suppressed")
			ld.Info("dummy")
			assert.Zerof(t, lf.lenr(), "suppressed")

			// rate-limiters should be independent
			ld.Error("dummy")
			assert.Contains(t, lf.readr(), "dummy")
			ld.Error("dummy")
			assert.Zerof(t, lf.lenr(), "suppressed")
			ld.Error("dummy")
			assert.Zerof(t, lf.lenr(), "suppressed")

			// wait until we get another token
			time.Sleep(15 * time.Millisecond)

			ld.Info("dummy")
			assert.Contains(t, lf.read(), "dummy")
			assert.Contains(t, lf.readr(), "suppressed 2 log")
			ld.Info("dummy")
			assert.Zerof(t, lf.lenr(), "suppressed")
			ld.Info("dummy")
			assert.Zerof(t, lf.lenr(), "suppressed")

			ld.Error("dummy")
			assert.Contains(t, lf.read(), "dummy")
			assert.Contains(t, lf.readr(), "suppressed 2 log")
			ld.Error("dummy")
			assert.Zerof(t, lf.lenr(), "suppressed")
			ld.Error("dummy")
			assert.Zerof(t, lf.lenr(), "suppressed")
		},
	},
	{
		name:          "rate-limited-logger-inactive-loggers-not-counted",
		addSuppressed: true,
		period:        10 * time.Millisecond,
		burst:         1,
		tester: func(t *testing.T, lf *LoggerFactory) {
			// reuse logger
			ld := lf.NewLogger(testScope)

			// Trace is inactive: no log
			ld.Trace("dummy")
			assert.Zerof(t, lf.lenr(), "inactive")
			ld.Trace("dummy")
			assert.Zerof(t, lf.lenr(), "suppressed")
			ld.Trace("dummy")
			assert.Zerof(t, lf.lenr(), "suppressed")

			// wait until we would get another token
			time.Sleep(15 * time.Millisecond)

			ld.Trace("dummy")
			assert.Zerof(t, lf.lenr(), "inactive")
			ld.Trace("dummy")
			assert.Zerof(t, lf.lenr(), "suppressed")
			ld.Trace("dummy")
			assert.Zerof(t, lf.lenr(), "suppressed")

			// increase loglevel
			lf.SetLevel(fmt.Sprintf("%s:TRACE", testScope))
			ld.Error("dummy")
			assert.Contains(t, lf.readr(), "dummy")
			ld.Error("dummy")
			assert.Zerof(t, lf.lenr(), "suppressed")
			ld.Error("dummy")
			assert.Zerof(t, lf.lenr(), "suppressed")
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
			loggerFactory := NewLoggerFactory("all:INFO")
			loggerFactory.Writer = &bytes.Buffer{}

			// create ratee-limiter logger
			_ = loggerFactory.NewRateLimitedLogger(testScope, c.period, c.burst, c.addSuppressed)

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
