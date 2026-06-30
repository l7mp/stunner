package offload

import (
	"context"
	"sync"
	"time"

	"github.com/pion/logging"

	"github.com/l7mp/stunner/internal/telemetry"
)

// OffloadStatsUpdateInterval is how often the reporter samples the engine and updates telemetry.
const OffloadStatsUpdateInterval = 2 * time.Second

// NameIndex returns the current offload name-hash to object-name maps for listeners and clusters.
// It is injected so the reporter need not depend on the runtime registry.
type NameIndex func() (listeners, clusters map[uint16]string)

// StatsReporter periodically samples an offload Engine and backfills the offloaded packet and byte
// totals into the shared listener/cluster telemetry counters. Offloaded traffic bypasses the
// userspace dataplane, so without this the packet/byte counters would undercount it.
type StatsReporter struct {
	engine    Engine
	tel       *telemetry.Telemetry
	nameIndex NameIndex
	log       logging.LeveledLogger
	interval  time.Duration

	cancel context.CancelFunc
	wg     sync.WaitGroup
	last   StatMap
}

// NewStatsReporter creates an offload telemetry reporter for the given engine.
func NewStatsReporter(engine Engine, tel *telemetry.Telemetry, nameIndex NameIndex, log logging.LeveledLogger) *StatsReporter {
	return &StatsReporter{
		engine:    engine,
		tel:       tel,
		nameIndex: nameIndex,
		log:       log,
		interval:  OffloadStatsUpdateInterval,
		last:      StatMap{},
	}
}

// Start launches the reporter loop in its own goroutine. It runs until Close is called.
func (r *StatsReporter) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		// A Timer reset after each report keeps a fixed gap between samples regardless of how long a
		// report takes (the engine's stat read has variable cost), so a slow report never bunches the
		// next one up against it.
		timer := time.NewTimer(r.interval)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				r.report()
				timer.Reset(r.interval)
			}
		}
	}()
}

// Close stops the reporter loop and waits for it to finish.
func (r *StatsReporter) Close() {
	if r.cancel != nil {
		r.cancel()
	}
	r.wg.Wait()
}

// report samples the engine and adds the per-object deltas since the last sample to the shared
// packet and byte counters. A sample that is smaller than the last one marks a counter reset (the
// engine re-pinned its maps), in which case the current value is reported as the delta.
func (r *StatsReporter) report() {
	if r.tel == nil {
		return
	}
	stats, err := r.engine.Stats()
	if err != nil {
		r.log.Debugf("could not read offload stats: %s", err.Error())
		return
	}
	if len(stats) == 0 && len(r.last) == 0 {
		return
	}

	listeners, clusters := r.nameIndex()

	for k, cur := range stats {
		connType := telemetry.ClusterType
		index := clusters
		if IsListener(k.Flags) {
			connType = telemetry.ListenerType
			index = listeners
		}
		name, ok := index[k.NameHash]
		if !ok {
			continue
		}

		dPkts, dBytes := cur.Pkts, cur.Bytes
		if prev, ok := r.last[k]; ok && cur.Pkts >= prev.Pkts && cur.Bytes >= prev.Bytes {
			dPkts -= prev.Pkts
			dBytes -= prev.Bytes
		}

		dir := telemetry.Outgoing
		if IsDirIn(k.Flags) {
			dir = telemetry.Incoming
		}
		if dPkts > 0 {
			r.tel.IncrementPackets(name, connType, dir, dPkts)
		}
		if dBytes > 0 {
			r.tel.IncrementBytes(name, connType, dir, dBytes)
		}
	}

	r.last = stats
}
