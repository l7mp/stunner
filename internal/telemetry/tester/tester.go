package tester

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// Tester provides testing utilities for OpenTelemetry metrics
type Tester struct {
	sdkmetric.Reader
	*testing.T
}

// New creates a new test helper and returns both the helper and a MeterProvider
// that can be used to initialize the system under test
func New(reader sdkmetric.Reader, t *testing.T) *Tester {
	return &Tester{Reader: reader, T: t}
}

// CollectAndCount returns the number of metrics with the given name and attributes
func (h *Tester) CollectAndCount(name string) int {
	h.Helper()

	metrics := &metricdata.ResourceMetrics{}
	err := h.Collect(context.Background(), metrics)
	assert.NoError(h, err, "failed to collect metrics: %v")

	for _, scope := range metrics.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}

			sum, ok := m.Data.(metricdata.Sum[int64])
			assert.True(h, ok, fmt.Sprintf("metric %s is not a Sum", name))

			return len(sum.DataPoints)
		}
	}

	return 0
}

// CollectAndGetInt returns the value of the metric with given name and attributes.
func (h *Tester) CollectAndGetInt(name string, attrs ...string) int {
	h.Helper()

	assert.True(h, len(attrs)%2 == 0, "odd number of attribute key-value pairs")

	metrics := &metricdata.ResourceMetrics{}
	err := h.Collect(context.Background(), metrics)
	assert.NoError(h, err, "failed to collect metrics: %v")

	for _, scope := range metrics.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}

			sum, ok := m.Data.(metricdata.Sum[int64])
			assert.True(h, ok, fmt.Sprintf("metric %s is not a Sum", name))

			for _, dp := range sum.DataPoints {
				matches := true
				for i := 0; i < len(attrs); i += 2 {
					if val, ok := dp.Attributes.Value(attribute.Key(attrs[i])); !ok || val.AsString() != attrs[i+1] {
						matches = false
						break
					}
				}
				if matches {
					return int(dp.Value)
				}
			}
		}
	}

	return 0
}

// CollectAndDump returns the metrics with the given name and attributes as a string.
func (h *Tester) CollectAndDump(name string, attrs ...string) string {
	h.Helper()

	metrics := &metricdata.ResourceMetrics{}
	if err := h.Collect(context.Background(), metrics); err != nil {
		return ""
	}

	ret := []string{}
	for _, scope := range metrics.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}

			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				return ""
			}

			for _, dp := range sum.DataPoints {
				matches := true
				for i := 0; i < len(attrs); i += 2 {
					if val, ok := dp.Attributes.Value(attribute.Key(attrs[i])); !ok || val.AsString() != attrs[i+1] {
						matches = false
						break
					}
				}
				if matches {
					ret = append(ret, fmt.Sprintf("%v", dp))
				}
			}
		}
	}

	return strings.Join(ret, ",")
}
