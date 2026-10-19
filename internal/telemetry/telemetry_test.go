package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/l7mp/stunner/v2/pkg/buildinfo"
	"github.com/l7mp/stunner/v2/pkg/logger"
)

func TestTelemetryNewDryRunResource(t *testing.T) {
	loggerFactory := logger.NewLoggerFactory("all:ERROR")
	tel, err := New(Callbacks{
		GetAllocationCount: func() int64 { return 0 },
	}, true, loggerFactory.NewLogger("test-telemetry"))
	require.NoError(t, err)
	require.NotNil(t, tel)
	t.Cleanup(func() {
		assert.NoError(t, tel.Close())
	})

	rm := &metricdata.ResourceMetrics{}
	err = tel.Collect(context.Background(), rm)
	require.NoError(t, err)

	serviceName := ""
	for _, kv := range rm.Resource.Attributes() {
		if kv.Key == attribute.Key("service.name") {
			serviceName = kv.Value.AsString()
			break
		}
	}
	assert.Equal(t, "stunner", serviceName)
}

// TestTelemetryBuildInfo pins the build info metric: a constant 1 labeled with the version.
func TestTelemetryBuildInfo(t *testing.T) {
	loggerFactory := logger.NewLoggerFactory("all:ERROR")
	tel, err := New(Callbacks{
		GetAllocationCount: func() int64 { return 0 },
		GetBuildInfo: func() buildinfo.BuildInfo {
			return buildinfo.BuildInfo{Version: "v2.0.0+ws.abc", CommitHash: "abc", BuildDate: "today"}
		},
	}, true, loggerFactory.NewLogger("test-telemetry"))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, tel.Close()) })

	rm := &metricdata.ResourceMetrics{}
	require.NoError(t, tel.Collect(context.Background(), rm))
	var point *metricdata.DataPoint[int64]
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if g, ok := m.Data.(metricdata.Gauge[int64]); ok && m.Name == "stunner_build_info" && len(g.DataPoints) == 1 {
				point = &g.DataPoints[0]
			}
		}
	}
	require.NotNil(t, point, "build info metric")
	assert.Equal(t, int64(1), point.Value)
	version, _ := point.Attributes.Value("version")
	assert.Equal(t, "v2.0.0+ws.abc", version.AsString())
	commit, _ := point.Attributes.Value("commit")
	assert.Equal(t, "abc", commit.AsString())
}

// TestTelemetryStats pins the traffic totals behind the status: the per-packet counters and the
// offload backfill add up per listener or cluster and direction.
func TestTelemetryStats(t *testing.T) {
	tel, err := New(Callbacks{GetAllocationCount: func() int64 { return 0 }}, true,
		logger.NewLoggerFactory("all:ERROR").NewLogger("test-telemetry"))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, tel.Close()) })

	c := tel.Counters("l", ListenerType)
	c.Add(Incoming, 100)
	c.Add(Incoming, 50)
	c.Add(Outgoing, 10)
	tel.IncrementPackets("l", ListenerType, Incoming, 3)
	tel.IncrementBytes("l", ListenerType, Incoming, 300)

	s := tel.Stats("l", ListenerType)
	assert.Equal(t, uint64(5), s.Rx.Pkts)
	assert.Equal(t, uint64(450), s.Rx.Bytes)
	assert.Equal(t, uint64(1), s.Tx.Pkts)
	assert.Equal(t, uint64(10), s.Tx.Bytes)
	assert.Zero(t, tel.Stats("l", ClusterType), "a cluster of the same name counts apart")
}
