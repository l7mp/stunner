package main

import (
	"net"
	"strings"

	"github.com/spf13/cobra"

	"github.com/l7mp/stunner/v2/pkg/udp"
)

// netCmd is the plain UDP or TCP stream: send streams packets to a peer and measures the echo,
// echo sends back what it receives and reports every flow. An RTSP stream interleaved over TCP is
// a TCP stream.
func (a *app) netCmd(network string) *cobra.Command {
	cmd := &cobra.Command{Use: network, Short: "Plain " + strings.ToUpper(network) + " streams"}

	send := &cobra.Command{
		Use:   "send <host>:<port>|k8s://<namespace>/<gateway>[/<listener>]",
		Short: "Send packets to the peer and measure the echo",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := a.context(cmd)
			defer cancel()
			addr, err := a.address(ctx, args[0])
			if err != nil {
				return err
			}
			c := a.config()
			c.Echo, c.ByteStream = true, network == "tcp"
			return a.send(ctx, &net.Dialer{}, network, addr, c)
		},
	}

	echo := &cobra.Command{
		Use:   "echo [<ip>]:<port>",
		Short: "Send back everything received and report every flow",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := a.context(cmd)
			defer cancel()
			var l net.Listener
			var err error
			if network == "udp" {
				l, err = udp.DefaultListenConfig.Listen("udp", args[0])
			} else {
				l, err = net.Listen("tcp", args[0])
			}
			if err != nil {
				return err
			}
			a.log.Infof("echoing %s at %s", strings.ToUpper(network), l.Addr())
			return a.serve(ctx, l, network == "udp")
		},
	}

	cmd.AddCommand(send, echo)
	return cmd
}
