package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/l7mp/stunner/pkg/logger"
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
