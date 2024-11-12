package telemetry

import (
	"context"
	"fmt"
	"time"

	"github.com/pion/logging"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

const (
	stunnerInstrumentName = "stunner"
	closeTimeout          = 250 * time.Millisecond
)

// Callbacks lets the caller to define various callbacks for reporting active metrics from an
// object that cannot be reached from this subpackage. This interface allows to easily add new
// metric reporters.
type Callbacks struct {
	// GetAllocationCount should map to the total allocation counter of the server.
	GetAllocationCount func() int64
}

type Telemetry struct {
	sdkmetric.Reader

	meter    metric.Meter
	provider *sdkmetric.MeterProvider
	ctx      context.Context
	cancel   context.CancelFunc

	// Metrics instruments
	ListenerPacketsCounter metric.Int64Counter
	ListenerBytesCounter   metric.Int64Counter
	ListenerConnsCounter   metric.Int64Counter
	ListenerConnsGauge     metric.Int64UpDownCounter
	ClusterPacketsCounter  metric.Int64Counter
	ClusterBytesCounter    metric.Int64Counter
	AllocationsGauge       metric.Int64ObservableGauge

	callbacks Callbacks

	log logging.LeveledLogger
}

func New(callbacks Callbacks, dryRun bool, log logging.LeveledLogger) (*Telemetry, error) {
	var reader sdkmetric.Reader

	resource, err := resource.Merge(resource.Default(),
		resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceNameKey.String("stunner")))
	if err != nil {
		return nil, fmt.Errorf("could not create OTEL resource: %w", err)
	}

	if dryRun {
		// Use manual collection mode
		reader = sdkmetric.NewManualReader()
	} else {
		// Create a new Prometheus exporter (starts background collection)
		exporter, err := prometheus.New()
		if err != nil {
			return nil, err
		}
		reader = exporter
	}

	// Create a new MeterProvider with the Prometheus exporter
	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(resource),
		sdkmetric.WithReader(reader),
	)

	ctx, cancel := context.WithCancel(context.Background())
	t := &Telemetry{
		Reader:    reader,
		meter:     provider.Meter(stunnerInstrumentName),
		provider:  provider,
		callbacks: callbacks,
		ctx:       ctx,
		cancel:    cancel,
		log:       log,
	}

	if err := t.init(); err != nil {
		return nil, err
	}
	return t, nil
}

// Close cleanly shuts down the meter provider and blocks until the shutdown cycle is finished or a
// timout expires.
func (t *Telemetry) Close() error {
	ctx, cancel := context.WithTimeout(t.ctx, closeTimeout)
	defer cancel()
	defer t.cancel()
	return t.provider.Shutdown(ctx)
}

func (t *Telemetry) Ctx() context.Context {
	return t.ctx
}

func (t *Telemetry) init() error {
	var err error

	// Initialize listener metrics
	t.ListenerPacketsCounter, err = t.meter.Int64Counter(
		stunnerInstrumentName+"_listener_packets_total",
		metric.WithDescription("Number of datagrams sent or received at a listener"),
	)
	if err != nil {
		return err
	}

	t.ListenerBytesCounter, err = t.meter.Int64Counter(
		stunnerInstrumentName+"_listener_bytes_total",
		metric.WithDescription("Number of bytes sent or received at a listener"),
	)
	if err != nil {
		return err
	}

	t.ListenerConnsCounter, err = t.meter.Int64Counter(
		stunnerInstrumentName+"_listener_connections_total",
		metric.WithDescription("Number of all downstream connections observed at a listener"),
	)
	if err != nil {
		return err
	}

	t.ListenerConnsGauge, err = t.meter.Int64UpDownCounter(
		stunnerInstrumentName+"_listener_connections",
		metric.WithDescription("Number of active downstream connections at a listener"),
	)
	if err != nil {
		return err
	}

	// Initialize cluster metrics
	t.ClusterPacketsCounter, err = t.meter.Int64Counter(
		stunnerInstrumentName+"_cluster_packets_total",
		metric.WithDescription("Number of datagrams sent to or received from backends"),
	)
	if err != nil {
		return err
	}

	t.ClusterBytesCounter, err = t.meter.Int64Counter(
		stunnerInstrumentName+"_cluster_bytes_total",
		metric.WithDescription("Number of bytes sent to or received from backends"),
	)
	if err != nil {
		return err
	}

	t.AllocationsGauge, err = t.meter.Int64ObservableGauge(
		stunnerInstrumentName+"_allocations_active",
		metric.WithDescription("Number of active allocations"),
	)
	if err != nil {
		return err
	}

	_, err = t.meter.RegisterCallback(
		func(_ context.Context, o metric.Observer) error {
			o.ObserveInt64(t.AllocationsGauge, t.callbacks.GetAllocationCount())
			return nil
		},
		t.AllocationsGauge,
	)
	if err != nil {
		return err
	}

	return nil
}

func (t *Telemetry) IncrementPackets(n string, c ConnType, d Direction, count uint64) {
	attrs := metric.WithAttributes(
		attribute.String("name", n),
		attribute.String("direction", d.String()),
	)

	switch c {
	case ListenerType:
		t.ListenerPacketsCounter.Add(t.ctx, int64(count), attrs)
	case ClusterType:
		t.ClusterPacketsCounter.Add(t.ctx, int64(count), attrs)
	}
}

func (t *Telemetry) IncrementBytes(n string, c ConnType, d Direction, count uint64) {
	attrs := metric.WithAttributes(
		attribute.String("name", n),
		attribute.String("direction", d.String()),
	)

	switch c {
	case ListenerType:
		t.ListenerBytesCounter.Add(t.ctx, int64(count), attrs)
	case ClusterType:
		t.ClusterBytesCounter.Add(t.ctx, int64(count), attrs)
	}
}

func (t *Telemetry) AddConnection(n string, c ConnType) {
	attrs := metric.WithAttributes(attribute.String("name", n))

	switch c {
	case ListenerType:
		t.ListenerConnsGauge.Add(t.ctx, 1, attrs)
		t.ListenerConnsCounter.Add(t.ctx, 1, attrs)
	case ClusterType:
		// Cluster connection metrics are disabled
	}
}

func (t *Telemetry) SubConnection(n string, c ConnType) {
	attrs := metric.WithAttributes(attribute.String("name", n))

	switch c {
	case ListenerType:
		t.ListenerConnsGauge.Add(t.ctx, -1, attrs)
	case ClusterType:
		// Cluster connection metrics are disabled
	}
}
