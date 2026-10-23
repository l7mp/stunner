// Command mediactl is a media source and sink for testing media gateways: it sends packets and
// measures what comes back, or answers and echoes, over SIP/RTP, UDP, TCP, TURN and SRT. It is a
// command line over pkg/mediactl and its subpackages.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"

	"github.com/pion/logging"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	cliopt "k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/l7mp/stunner/v2/pkg/logger"
	"github.com/l7mp/stunner/v2/pkg/mediactl"
	"github.com/l7mp/stunner/v2/pkg/mediactl/k8s"
	"github.com/l7mp/stunner/v2/pkg/utils/discovery"
)

// env is the prefix of the environment variables that set the flags: MEDIACTL_TIMEOUT sets
// --timeout.
const env = "MEDIACTL"

// app is one run of mediactl: its settings, from the flags, the environment and the config file in
// this order, its logger, and the resolver of its k8s:// addresses.
type app struct {
	v   *viper.Viper
	lf  logger.LoggerFactory
	log logging.LeveledLogger
	k8s k8s.Resolver
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := newRootCmd().ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}

// newRootCmd builds the command tree of a run.
func newRootCmd() *cobra.Command {
	a := &app{v: viper.New(), k8s: k8s.Resolver{K8s: cliopt.NewConfigFlags(true), CDS: discovery.NewCDSConfigFlags()}}
	root := &cobra.Command{
		Use:   "mediactl",
		Short: "A media source and sink for testing media gateways",
		Long: "mediactl sends packets and measures what comes back, or answers and echoes: SIP calls " +
			"with RTP media, UDP and TCP streams (RTSP is TCP), TURN relayed streams and SRT streams. " +
			"An address may be k8s://<namespace>/<gateway>[/<listener>], the public address of a STUNner " +
			"Gateway listener, and a TURN peer k8s://<namespace>/<service>:<port>, the first ready pod of a Service.",
		SilenceUsage:      true,
		DisableAutoGenTag: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			a.v.SetEnvPrefix(env)
			a.v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
			a.v.AutomaticEnv()
			if err := a.v.BindPFlags(cmd.Flags()); err != nil {
				return err
			}
			if f := a.v.GetString("config"); f != "" {
				a.v.SetConfigFile(f)
				if err := a.v.ReadInConfig(); err != nil {
					return err
				}
			}
			level := a.v.GetString("log")
			if a.v.GetBool("verbose") {
				level = "all:DEBUG"
			}
			a.lf = logger.NewLoggerFactory(level)
			if a.v.GetString("log-format") == "json" {
				a.lf = logger.NewJSONLoggerFactory(level)
			}
			a.log = a.lf.NewLogger("mediactl")
			a.k8s.Log = a.lf.NewLogger("k8s")
			return nil
		},
	}
	f := root.PersistentFlags()
	f.String("config", "", "config file setting the flags (YAML, JSON or TOML)")
	f.StringP("log", "l", "all:INFO", "log level (format: <scope>:<level>)")
	f.BoolP("verbose", "v", false, "verbose logging, identical to <-l all:DEBUG>")
	f.String("log-format", "text", `log format: "text" or "json"`)
	f.DurationP("timeout", "t", 0, "stop after this long; 0 runs until interrupted")
	f.Int("length", mediactl.DefaultLength, "bytes per packet a sender sends")
	f.Float64P("send-rate", "k", mediactl.DefaultRate, "packets per second a sender sends (writes per second over TCP)")
	f.IntP("parallel", "P", 1, "the number of streams a sender runs at once")
	f.StringP("output", "o", "text", `the results of a sender: "text" (logged) or "json" (on stdout)`)
	f.Float64("max-loss", -1, "fail a sender losing more than this percent of its packets; negative: never")
	f.Duration("max-rtt", 0, "fail an echoing sender whose average round trip is longer; 0: never")
	f.Bool("voip", false, "report the call quality of an echoing sender as the E-model rates a G.711 voice call: R-factor and MOS")
	f.String("ts", "", "stream the packets of this file (an MPEG-TS file, say) instead of measured "+
		"packets, from the start again at its end; nothing is measured but the bitrate")
	a.k8s.K8s.AddFlags(f)
	a.k8s.CDS.AddFlags(f)

	root.AddCommand(a.sipCmd(), a.netCmd("udp"), a.netCmd("tcp"), a.turnCmd(), a.srtCmd())
	return root
}

// context returns the context of a run: the command's, ended after the timeout if one is set.
func (a *app) context(cmd *cobra.Command) (context.Context, context.CancelFunc) {
	if d := a.v.GetDuration("timeout"); d > 0 {
		return context.WithTimeout(cmd.Context(), d)
	}
	return context.WithCancel(cmd.Context())
}

// config returns the stream config of the flags, logging to the run's logger.
func (a *app) config() mediactl.Config {
	return mediactl.Config{Length: a.v.GetInt("length"), Rate: a.v.GetFloat64("send-rate"),
		VoIP: a.v.GetBool("voip"), Logger: a.log}
}

// address resolves a k8s:// Gateway address to the listener's public host:port; any other
// address is returned as it is.
func (a *app) address(ctx context.Context, addr string) (string, error) {
	res, err := a.k8s.Address(ctx, addr)
	if err == nil && res != addr {
		a.log.Infof("%s is %s", addr, res)
	}
	return res, err
}

// send runs --parallel streams of a dialer to an address at once, then reports them, on the log
// or as JSON, and judges them: a stream expecting media back fails when it got nothing, or more
// loss or a longer round trip than allowed.
func (a *app) send(ctx context.Context, d mediactl.Dialer, network, address string, c mediactl.Config) error {
	n := max(1, a.v.GetInt("parallel"))
	stats, errs := make([]*mediactl.Stats, n), make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			conn, err := d.DialContext(ctx, network, address)
			if err != nil {
				errs[i] = err
				a.log.Errorf("stream %d: %s", i, err.Error())
				return
			}
			c := c
			if n > 1 {
				c.Logger = nil
			}
			if f := a.v.GetString("ts"); f != "" {
				src, err := os.Open(f)
				if err != nil {
					errs[i] = err
					_ = conn.Close()
					return
				}
				defer src.Close() //nolint:errcheck
				c.Source = &looping{ReadSeeker: src}
			}
			stats[i] = mediactl.Stream(ctx, conn, c)
			if n > 1 {
				a.log.Infof("stream %d: %s", i, stats[i])
			}
		})
	}
	wg.Wait()

	results := []mediactl.Result{}
	for _, s := range stats {
		if s != nil {
			results = append(results, s.Result())
		}
	}
	total := mediactl.Total(results)
	if n > 1 {
		a.log.Infof("total of %d streams: %s", len(results), total)
	}
	if a.v.GetString("output") == "json" {
		b, err := json.MarshalIndent(struct {
			Streams []mediactl.Result `json:"streams"`
			Total   mediactl.Result   `json:"total"`
		}{results, total}, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(b))
	}

	if err := errors.Join(errs...); err != nil {
		return err
	}
	return mediactl.Check(total, c, a.v.GetFloat64("max-loss"), a.v.GetDuration("max-rtt"))
}

// looping reads a file from the start again at its end.
type looping struct{ io.ReadSeeker }

func (l *looping) Read(p []byte) (int, error) {
	n, err := l.ReadSeeker.Read(p)
	if errors.Is(err, io.EOF) && n == 0 {
		if _, err := l.Seek(0, io.SeekStart); err != nil {
			return 0, err
		}
		return l.ReadSeeker.Read(p)
	}
	return n, err
}

// serve echoes the conns of a listener until the run ends.
func (a *app) serve(ctx context.Context, l net.Listener, datagram bool) error {
	go func() { <-ctx.Done(); _ = l.Close() }()
	if err := mediactl.Serve(l, datagram, a.log); ctx.Err() == nil {
		return err
	}
	return nil
}
