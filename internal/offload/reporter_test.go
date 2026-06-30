package offload

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/internal/telemetry"
	"github.com/l7mp/stunner/internal/telemetry/tester"
	"github.com/l7mp/stunner/pkg/logger"
)

// reporterHarness wires a StatsReporter to an offload Engine with a controllable name index and a
// telemetry Tester for assertions.
type reporterHarness struct {
	reporter  *StatsReporter
	tester    *tester.Tester
	listeners map[uint16]string
	clusters  map[uint16]string
}

// newReporterHarness builds a harness around the given engine. Telemetry runs in dry-run mode so it
// uses a manual reader the Tester can collect from.
func newReporterHarness(t *testing.T, eng Engine) *reporterHarness {
	t.Helper()
	loggerFactory := logger.NewLoggerFactory("all:ERROR")
	tel, err := telemetry.New(telemetry.Callbacks{GetAllocationCount: func() int64 { return 0 }},
		true, loggerFactory.NewLogger("tel-test"))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, tel.Close()) })

	h := &reporterHarness{
		listeners: map[uint16]string{},
		clusters:  map[uint16]string{},
	}
	h.reporter = NewStatsReporter(eng, tel,
		func() (map[uint16]string, map[uint16]string) { return h.listeners, h.clusters },
		loggerFactory.NewLogger("reporter-test"))
	h.tester = tester.New(tel, t)
	return h
}

// withListener registers a listener name so the reporter can resolve its stat hashes.
func (h *reporterHarness) withListener(name string) *reporterHarness {
	h.listeners[NameHash(name)] = name
	return h
}

// withCluster registers a cluster name so the reporter can resolve its stat hashes.
func (h *reporterHarness) withCluster(name string) *reporterHarness {
	h.clusters[NameHash(name)] = name
	return h
}

// report triggers a single synchronous stats sample.
func (h *reporterHarness) report() { h.reporter.report() }

// fakeEngine is a controllable offload.Engine for the open-source reporter test: it reports whatever
// StatMap is set on it, with no eBPF dependency.
type fakeEngine struct {
	stats StatMap
}

func (f *fakeEngine) Name() string                              { return "fake" }
func (f *fakeEngine) Start(_ string, _ []string) error          { return nil }
func (f *fakeEngine) Close() error                              { return nil }
func (f *fakeEngine) Upsert(_, _ Connection, _, _ string) error { return nil }
func (f *fakeEngine) Remove(_, _ Connection) error              { return nil }
func (f *fakeEngine) Stats() (StatMap, error)                   { return f.stats, nil }

// statKey builds an offload stat key for the given name and flags.
func statKey(name string, listener, dirIn bool) StatKey {
	var flags uint8
	if listener {
		flags |= FlagListener
	}
	if dirIn {
		flags |= FlagDirIn
	}
	return StatKey{NameHash: NameHash(name), Flags: flags}
}

func TestStatsReporterReport(t *testing.T) {
	eng := &fakeEngine{stats: StatMap{}}
	h := newReporterHarness(t, eng).withListener("li1").withCluster("cl3")

	t.Run("empty sample reports nothing", func(t *testing.T) {
		h.report()
		assert.Equal(t, 0, h.tester.CollectAndCount("stunner_listener_packets_total"))
		assert.Equal(t, 0, h.tester.CollectAndCount("stunner_listener_bytes_total"))
		assert.Equal(t, 0, h.tester.CollectAndCount("stunner_cluster_packets_total"))
		assert.Equal(t, 0, h.tester.CollectAndCount("stunner_cluster_bytes_total"))
	})

	t.Run("first sample reports the absolute totals", func(t *testing.T) {
		eng.stats = StatMap{
			statKey("li1", true, false): {Pkts: 1, Bytes: 54}, // listener tx
			statKey("cl3", false, true): {Pkts: 1, Bytes: 49}, // cluster rx
		}
		h.report()

		assert.Equal(t, 1, h.tester.CollectAndGetInt("stunner_listener_packets_total", "name", "li1", "direction", "tx"))
		assert.Equal(t, 54, h.tester.CollectAndGetInt("stunner_listener_bytes_total", "name", "li1", "direction", "tx"))
		assert.Equal(t, 1, h.tester.CollectAndGetInt("stunner_cluster_packets_total", "name", "cl3", "direction", "rx"))
		assert.Equal(t, 49, h.tester.CollectAndGetInt("stunner_cluster_bytes_total", "name", "cl3", "direction", "rx"))
	})

	t.Run("next sample adds only the delta", func(t *testing.T) {
		eng.stats = StatMap{
			statKey("li1", true, false): {Pkts: 3, Bytes: 154}, // +2 pkts, +100 bytes
			statKey("cl3", false, true): {Pkts: 1, Bytes: 49},  // unchanged
		}
		h.report()

		// the cumulative counter reflects the new absolute total
		assert.Equal(t, 3, h.tester.CollectAndGetInt("stunner_listener_packets_total", "name", "li1", "direction", "tx"))
		assert.Equal(t, 154, h.tester.CollectAndGetInt("stunner_listener_bytes_total", "name", "li1", "direction", "tx"))
		assert.Equal(t, 1, h.tester.CollectAndGetInt("stunner_cluster_packets_total", "name", "cl3", "direction", "rx"))
	})

	t.Run("counter reset adds the new absolute", func(t *testing.T) {
		// the engine re-pinned its maps, so the sample is smaller than the last one: the reporter must
		// add the current value rather than underflow.
		eng.stats = StatMap{
			statKey("li1", true, false): {Pkts: 1, Bytes: 10},
		}
		h.report()
		// previous cumulative was 3 pkts; the reset adds 1 -> 4
		assert.Equal(t, 4, h.tester.CollectAndGetInt("stunner_listener_packets_total", "name", "li1", "direction", "tx"))
	})

	t.Run("unknown name is skipped", func(t *testing.T) {
		before := h.tester.CollectAndCount("stunner_listener_packets_total")
		eng.stats = StatMap{
			statKey("unknown", true, false): {Pkts: 5, Bytes: 5},
		}
		h.report()
		assert.Equal(t, before, h.tester.CollectAndCount("stunner_listener_packets_total"))
	})
}

// TestStatsReporterLoop exercises the timer-driven loop end to end: with a short interval the
// reporter samples the engine on its own, picks up the stats, and Close shuts it down promptly.
// This is the pattern the premium eBPF tests use — generate real traffic, then poll the counters
// via Eventually rather than racing a single synchronous report against the eBPF map update. The
// interval is an instance field, so same-package tests shorten it without a mutable global.
func TestStatsReporterLoop(t *testing.T) {
	eng := &fakeEngine{stats: StatMap{
		statKey("li1", true, false): {Pkts: 7, Bytes: 700},
	}}
	h := newReporterHarness(t, eng).withListener("li1")
	h.reporter.interval = 5 * time.Millisecond

	h.reporter.Start()
	assert.Eventually(t, func() bool {
		return h.tester.CollectAndGetInt("stunner_listener_packets_total", "name", "li1", "direction", "tx") == 7
	}, time.Second, 5*time.Millisecond)

	done := make(chan struct{})
	go func() { h.reporter.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close did not return promptly")
	}
}
