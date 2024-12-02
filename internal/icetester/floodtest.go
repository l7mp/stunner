package icetester

import (
	"context"
	"encoding/binary"
	"net"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"gonum.org/v1/gonum/stat"
)

const (
	MaxPacketCount = 10000
)

type Stats struct {
	SendRate        float64 // packets per second
	LossRate        float64 // percentage
	MeanLatency     float64 // milliseconds
	MedianLatency   float64 // milliseconds
	P95Latency      float64 // milliseconds
	P99Latency      float64 // milliseconds
	PacketsSent     uint32
	PacketsReceived uint32
	Duration        time.Duration
}

type Packet struct {
	SeqNum    uint32
	Timestamp int64
}

func FloodTest(ctx context.Context, conn net.Conn, interval time.Duration, packetSize int) (*Stats, error) {
	// Prepare buffer pool
	bufferPool := sync.Pool{
		New: func() interface{} {
			return make([]byte, packetSize)
		},
	}

	// Stats
	received := make(map[uint32]int64)
	latencies := make([]float64, 0)

	// Atomic counter for sequence numbers
	var currentSeq uint32

	// Start receiver goroutine
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		buffer := bufferPool.Get().([]byte)
		defer bufferPool.Put(buffer) //nolint:staticcheck

		for {
			_, err := conn.Read(buffer)
			if err != nil {
				return
			}

			seqNum := binary.BigEndian.Uint32(buffer[0:4])
			timestamp := int64(binary.BigEndian.Uint64(buffer[4:12]))
			received[seqNum] = timestamp
			latency := float64(time.Now().UnixNano()-timestamp) / float64(time.Millisecond)
			latencies = append(latencies, latency)
		}
	}()

	// Start sender goroutine
	wg.Add(1)
	var startTime, endTime time.Time
	go func() {
		defer wg.Done()
		startTime = time.Now()
		defer func() { endTime = time.Now() }()

		for {
			buffer := bufferPool.Get().([]byte)
			seq := atomic.AddUint32(&currentSeq, 1) - 1

			binary.BigEndian.PutUint32(buffer[0:4], seq)
			binary.BigEndian.PutUint64(buffer[4:12], uint64(time.Now().UnixNano()))

			// Fill rest of buffer with sequence number for verification
			for j := 12; j < packetSize; j++ {
				buffer[j] = byte(seq)
			}

			_, err := conn.Write(buffer)
			if err != nil {
				return
			}
			bufferPool.Put(buffer) //nolint:staticcheck

			if interval != 0 {
				select {
				case <-time.After(interval):
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	// Wait for test duration plus a grace period for receiving remaining packets
	<-ctx.Done()
	conn.Close() // this will stop the goroutines

	wg.Wait()

	// Calculate statistics
	packetsSent := atomic.LoadUint32(&currentSeq)
	duration := endTime.Sub(startTime)
	stats := &Stats{
		PacketsSent:     packetsSent,
		PacketsReceived: uint32(len(received)),
		Duration:        duration,
		SendRate:        float64(packetsSent) / duration.Seconds(),
		LossRate:        (float64(packetsSent) - float64(len(received))) / float64(packetsSent) * 100,
	}

	sort.Float64s(latencies)
	if len(latencies) > 0 {
		// Convert slice to float64 slice if not already
		stats.MeanLatency = stat.Mean(latencies, nil)
		stats.MedianLatency = stat.Quantile(0.5, stat.Empirical, latencies, nil)
		stats.P95Latency = stat.Quantile(0.95, stat.Empirical, latencies, nil)
		stats.P99Latency = stat.Quantile(0.99, stat.Empirical, latencies, nil)
	}

	return stats, nil
}
