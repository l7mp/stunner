package main

import (
	"github.com/spf13/cobra"

	"github.com/l7mp/stunner/v2/pkg/mediactl/srt"
)

// srtCmd is the SRT caller and the SRT server. The caller publishes a stream, reads one and
// measures it, or sends packets and measures the echo; the server passes every published stream
// to its readers and echoes the flows that neither publish nor read.
func (a *app) srtCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "srt", Short: "SRT streams in live mode"}
	cmd.PersistentFlags().String("passphrase", "", "the passphrase of the stream encryption; empty: none")

	// caller makes the command of an SRT caller in a mode
	caller := func(use, short string, m srt.Mode) *cobra.Command {
		return &cobra.Command{
			Use:   use + " <host>:<port>|k8s://<namespace>/<gateway>[/<listener>] <stream>",
			Short: short,
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				ctx, cancel := a.context(cmd)
				defer cancel()
				addr, err := a.address(ctx, args[0])
				if err != nil {
					return err
				}
				a.log.Infof("calling %s with stream ID %q", addr, srt.StreamID(args[1], m))
				d := srt.Dialer{Stream: args[1], Mode: m, Passphrase: a.v.GetString("passphrase")}
				return a.send(ctx, d, "srt", addr, srt.Config(m, a.config()))
			},
		}
	}

	serve := &cobra.Command{
		Use:   "serve [<ip>]:<port>",
		Short: "Pass published streams to their readers, echo every other flow",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := a.context(cmd)
			defer cancel()
			l, err := srt.Listen(args[0], a.v.GetString("passphrase"), a.log)
			if err != nil {
				return err
			}
			a.log.Infof("serving SRT at %s", l.Addr())
			return a.serve(ctx, l, true)
		},
	}

	cmd.AddCommand(
		caller("publish", "Publish a stream of packets", srt.Publish),
		caller("read", "Read a stream and measure what arrives", srt.Request),
		caller("send", "Send packets on a stream and measure the echo", srt.Echo),
		serve)
	return cmd
}
