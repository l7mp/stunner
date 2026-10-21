// Package mediactl holds the media sources and sinks of the mediactl tool and the tests: packet
// streams that measure the bitrate, the loss and the round trip, and echo servers, over any
// net.Conn. Every protocol is a Dialer of the conns of a sender and a net.Listener of the conns of
// an answerer: net.Dialer and pkg/udp or net.Listen for UDP and TCP, and the subpackages for the
// rest, sip (SIP calls, their RTP media framed by rtp), srt (SRT callers and listeners) and
// turn (conns relayed through a TURN server).
package mediactl

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/pion/logging"
)

// HeaderLen is the header a stream writes at the start of every packet long enough: a sequence
// number and the send time, so the receiver can count the loss and the sender the round trip of
// the echo.
const HeaderLen = 12

// The stream of a sender by default: a packet of 160 bytes 50 times a second, the size and the
// rate of 20 ms G.711 frames.
const (
	DefaultLength = 160
	DefaultRate   = 50
)

// ErrNothingReceived fails a stream that expected packets and got none.
var ErrNothingReceived = errors.New("nothing received")

// The timers of the streams and the echo servers, variables so the tests can shorten them.
var (
	// ReportInterval is how often a stream logs its rates.
	ReportInterval = time.Second
	// EchoDrain is how long an echoing stream waits for the echoes still on the way back once it
	// stopped sending.
	EchoDrain = 500 * time.Millisecond
	// FlowIdle is how long a datagram echo waits for the next packet before it ends the flow
	// and reports it.
	FlowIdle = 3 * time.Second
)

// Config is the configuration of a stream.
type Config struct {
	// Length is the bytes per packet the stream sends, Rate the packets per second; a zero
	// Rate sends nothing.
	Length int
	Rate   float64
	// Echo tells that what comes back is the echo of what the stream sent: the loss is what did
	// not come back, else the gaps in the sequence numbers received.
	Echo bool
	// ByteStream tells that the conn is a byte stream (TCP): a packet is a read or a write, no
	// header is read and nothing is lost.
	ByteStream bool
	// Source, if set, fills every packet sent instead of the header and the padding: real media,
	// such as an MPEG-TS file. The receiver measures no loss and no round trip then; a short read
	// sends a short packet, an error ends the stream.
	Source io.Reader
	// Logger logs the rates of every ReportInterval and the summary; nil logs nothing.
	Logger logging.LeveledLogger
}

// Stats count a stream: the packets and bytes sent and received, and over the received packets
// that carry a header, the highest sequence number and the round trip.
type Stats struct {
	mu                               sync.Mutex
	sending, echo, byteStream        bool
	start, end, last                 time.Time // the start, the end of sending, the last packet received
	sent, sentBytes, recv, recvBytes int64
	maxSeq                           int64 // -1 before the first header
	rttN                             int64
	rttSum, rttMin, rttMax           time.Duration
}

// NewStats returns the stats of a receiver alone, such as an echo server, starting now.
func NewStats() *Stats { return &Stats{start: time.Now(), maxSeq: -1} }

// Received counts a received packet, reading its header if parse is set.
func (s *Stats) Received(p []byte, parse bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recv++
	s.recvBytes += int64(len(p))
	s.last = time.Now()
	if !parse || len(p) < HeaderLen {
		return
	}
	s.maxSeq = max(s.maxSeq, int64(binary.BigEndian.Uint32(p)))
	rtt := time.Since(time.Unix(0, int64(binary.BigEndian.Uint64(p[4:]))))
	if s.rttN == 0 || rtt < s.rttMin {
		s.rttMin = rtt
	}
	s.rttMax = max(s.rttMax, rtt)
	s.rttSum += rtt
	s.rttN++
}

// Result is what a stream measured. Lost and Expected are zero when the stream cannot tell
// (nothing to compare against, or a byte stream), the round trip when nothing echoed came back.
// It marshals to JSON with the durations as strings.
type Result struct {
	// Duration is up to the last packet sent, for a receiver alone up to the last received.
	Duration                                 time.Duration
	Sent, SentBytes, Received, ReceivedBytes int64
	Lost, Expected                           int64
	RTTMin, RTTAvg, RTTMax                   time.Duration
}

// MarshalJSON renders the result with snake case names and the durations as strings.
func (r Result) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Duration      string `json:"duration"`
		Sent          int64  `json:"sent"`
		SentBytes     int64  `json:"sent_bytes"`
		Received      int64  `json:"received"`
		ReceivedBytes int64  `json:"received_bytes"`
		Lost          int64  `json:"lost"`
		Expected      int64  `json:"expected"`
		RTTMin        string `json:"rtt_min,omitempty"`
		RTTAvg        string `json:"rtt_avg,omitempty"`
		RTTMax        string `json:"rtt_max,omitempty"`
	}{r.Duration.String(), r.Sent, r.SentBytes, r.Received, r.ReceivedBytes, r.Lost, r.Expected,
		durationOrEmpty(r.RTTMin), durationOrEmpty(r.RTTAvg), durationOrEmpty(r.RTTMax)})
}

func durationOrEmpty(d time.Duration) string {
	if d == 0 {
		return ""
	}
	return d.String()
}

// Total adds up the results of parallel streams: the counters summed, the longest duration, the
// round trip over all of them, its average weighted by the packets received.
func Total(rs []Result) Result {
	var t Result
	var weighted, weight int64
	for _, r := range rs {
		t.Duration = max(t.Duration, r.Duration)
		t.Sent += r.Sent
		t.SentBytes += r.SentBytes
		t.Received += r.Received
		t.ReceivedBytes += r.ReceivedBytes
		t.Lost += r.Lost
		t.Expected += r.Expected
		if r.RTTMax == 0 {
			continue
		}
		if t.RTTMin == 0 || r.RTTMin < t.RTTMin {
			t.RTTMin = r.RTTMin
		}
		t.RTTMax = max(t.RTTMax, r.RTTMax)
		weighted += int64(r.RTTAvg) * r.Received
		weight += r.Received
	}
	if weight > 0 {
		t.RTTAvg = time.Duration(weighted / weight)
	}
	return t
}

// String reports a result: what was sent, what was received, the loss and the round trip, each
// where it means something.
func (r Result) String() string {
	parts := []string{fmt.Sprintf("%.1fs", r.Duration.Seconds())}
	if r.Sent > 0 {
		parts = append(parts, fmt.Sprintf("sent %s pkts %s %s", count(r.Sent), humanize.Bytes(uint64(r.SentBytes)),
			Rate(r.SentBytes, r.Duration)))
	}
	// a publisher receives nothing, and expects nothing back
	if r.Received > 0 || r.Sent == 0 || r.Expected > 0 {
		parts = append(parts, fmt.Sprintf("received %s pkts %s %s", count(r.Received),
			humanize.Bytes(uint64(r.ReceivedBytes)), Rate(r.ReceivedBytes, r.Duration)))
	}
	if r.Expected > 0 {
		parts = append(parts, fmt.Sprintf("lost %s/%s (%.2f%%)", count(r.Lost), count(r.Expected),
			100*float64(r.Lost)/float64(r.Expected)))
	}
	if r.RTTMax > 0 {
		parts = append(parts, fmt.Sprintf("rtt min/avg/max %s/%s/%s", r.RTTMin.Round(time.Microsecond),
			r.RTTAvg.Round(time.Microsecond), r.RTTMax.Round(time.Microsecond)))
	}
	return strings.Join(parts, ", ")
}

// Result returns what the stream measured so far.
func (s *Stats) Result() Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := Result{Duration: time.Since(s.start), Sent: s.sent, SentBytes: s.sentBytes, Received: s.recv,
		ReceivedBytes: s.recvBytes}
	switch {
	case s.sending && !s.end.IsZero():
		r.Duration = s.end.Sub(s.start)
	case !s.sending && !s.last.IsZero():
		r.Duration = s.last.Sub(s.start)
	}
	if !s.byteStream {
		r.Expected = s.maxSeq + 1
		if s.echo {
			r.Expected = s.sent
		}
		r.Lost = max(r.Expected-s.recv, 0)
	}
	if s.rttN > 0 && s.echo {
		r.RTTMin, r.RTTAvg, r.RTTMax = s.rttMin, s.rttSum/time.Duration(s.rttN), s.rttMax
	}
	return r
}

// Rate formats bytes over a duration as a bitrate with an SI prefix: 75.64 kbit/s, 7.56 Mbit/s.
func Rate(bytes int64, d time.Duration) string {
	return humanize.SIWithDigits(float64(bytes*8)/d.Seconds(), 2, "bit/s")
}

// count formats a number of packets with an SI prefix: 500, 9.4k, 5.9M.
func count(n int64) string {
	return strings.ReplaceAll(humanize.SIWithDigits(float64(n), 1, ""), " ", "")
}

// String reports what the stream measured.
func (s *Stats) String() string { return s.Result().String() }

// Check judges the result of a sender streaming by c: a publisher fails when it sent nothing, a
// stream expecting packets when it got nothing, or lost more than maxLoss percent of its packets
// (a negative maxLoss never fails), or its average round trip is longer than maxRTT (0 never
// fails).
func Check(r Result, c Config, maxLoss float64, maxRTT time.Duration) error {
	if !c.Echo && c.Rate > 0 {
		if r.Sent == 0 {
			return errors.New("nothing sent")
		}
		return nil
	}
	if r.Received == 0 {
		return ErrNothingReceived
	}
	if maxLoss >= 0 && r.Expected > 0 && 100*float64(r.Lost)/float64(r.Expected) > maxLoss {
		return fmt.Errorf("lost %d of %d packets, more than %.2f%%", r.Lost, r.Expected, maxLoss)
	}
	if maxRTT > 0 && r.RTTAvg > maxRTT {
		return fmt.Errorf("average round trip %s, longer than %s", r.RTTAvg, maxRTT)
	}
	return nil
}

// Stream sends a packet every 1/Rate seconds on conn and counts what it reads, until the context
// ends or the conn fails, then closes the conn. An echoing stream waits EchoDrain for the echoes
// on the way back before it closes.
func Stream(ctx context.Context, conn io.ReadWriteCloser, c Config) *Stats {
	s := NewStats()
	s.sending, s.echo, s.byteStream = c.Rate > 0, c.Echo, c.ByteStream
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 65536)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			s.Received(buf[:n], !c.ByteStream)
		}
	}()

	var tick <-chan time.Time
	if s.sending {
		t := time.NewTicker(time.Duration(float64(time.Second) / c.Rate))
		defer t.Stop()
		tick = t.C
	}
	var report <-chan time.Time
	if c.Logger != nil {
		t := time.NewTicker(ReportInterval)
		defer t.Stop()
		report = t.C
	}
	pkt := make([]byte, c.Length)
	var prev Result
	prevAt := s.start
loop:
	for seq := uint32(0); ; {
		select {
		case <-ctx.Done():
			break loop
		case <-done:
			break loop
		case now := <-report:
			r := s.Result()
			c.Logger.Infof("interval %4.1f-%4.1fs: sent %s pkts %s, received %s pkts %s",
				prevAt.Sub(s.start).Seconds(), now.Sub(s.start).Seconds(),
				count(r.Sent-prev.Sent), Rate(r.SentBytes-prev.SentBytes, now.Sub(prevAt)),
				count(r.Received-prev.Received), Rate(r.ReceivedBytes-prev.ReceivedBytes, now.Sub(prevAt)))
			prev, prevAt = r, now
		case <-tick:
			out := pkt
			if c.Source != nil {
				n, err := io.ReadFull(c.Source, pkt)
				if n == 0 {
					if c.Logger != nil && err != nil {
						c.Logger.Debugf("source: %s", err.Error())
					}
					break loop
				}
				out = pkt[:n]
			} else if len(pkt) >= HeaderLen {
				binary.BigEndian.PutUint32(pkt, seq)
				binary.BigEndian.PutUint64(pkt[4:], uint64(time.Now().UnixNano()))
			}
			n, err := conn.Write(out)
			if err != nil {
				if c.Logger != nil {
					c.Logger.Debugf("write: %s", err.Error())
				}
				break loop
			}
			seq++
			s.mu.Lock()
			s.sent++
			s.sentBytes += int64(n)
			s.mu.Unlock()
		}
	}
	s.mu.Lock()
	s.end = time.Now()
	s.mu.Unlock()
	if s.sending && c.Echo {
		select {
		case <-done:
		case <-time.After(EchoDrain):
		}
	}
	_ = conn.Close()
	<-done
	if c.Logger != nil {
		c.Logger.Infof("summary %s", s)
	}
	return s
}
