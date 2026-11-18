package main

import (
	pionturn "github.com/pion/turn/v5"
	"github.com/spf13/cobra"

	"github.com/l7mp/stunner/v2/pkg/mediactl/k8s"
	"github.com/l7mp/stunner/v2/pkg/mediactl/turn"
)

// turnCmd is the TURN client: it relays packets through a TURN server to a peer and measures the
// echo.
func (a *app) turnCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "turn", Short: "Streams relayed through a TURN server (client only)"}

	send := &cobra.Command{
		Use:   "send <turn-uri>|k8s://<namespace>/<gateway>[/<listener>] <host>:<port>|k8s://<namespace>/<service>:<port>",
		Short: "Send packets to the peer through a TURN relay and measure the echo",
		Long: "Send packets to the peer through a TURN relay and measure the echo. The TURN URI is " +
			"turn:<host>:<port>?transport=udp|tcp, or turns: for TLS and DTLS; a STUNner Gateway " +
			"listener given as k8s://... brings its credentials. The relay is UDP, or with --tcp an " +
			"RFC 6062 TCP connection over a TCP or TLS transport.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := a.context(cmd)
			defer cancel()
			d := turn.Dialer{URI: args[0]}
			if k8s.IsAddress(args[0]) {
				var err error
				if d, err = a.k8s.TURNDialer(ctx, args[0]); err != nil {
					return err
				}
				a.log.Infof("%s is %s", args[0], d.URI)
			}
			if u := a.v.GetString("username"); u != "" {
				d.Username, d.Password = u, a.v.GetString("password")
			}
			d.Insecure, d.LoggerFactory = a.v.GetBool("insecure"), a.lf
			family, err := k8s.NewFamily(a.v.GetString("requested-address-family"))
			if err != nil {
				return err
			}
			switch family {
			case k8s.IPv4:
				d.RequestedFamily = pionturn.RequestedAddressFamilyIPv4
			case k8s.IPv6:
				d.RequestedFamily = pionturn.RequestedAddressFamilyIPv6
			}
			peer := args[1]
			if k8s.IsAddress(peer) {
				if _, peer, err = a.k8s.Peer(ctx, peer); err != nil {
					return err
				}
				a.log.Infof("%s is %s", args[1], peer)
			}
			network := "udp"
			if a.v.GetBool("tcp") {
				network = "tcp"
			}
			c := a.config()
			c.Echo, c.ByteStream = true, network == "tcp"
			return a.send(ctx, d, network, peer, c)
		},
	}
	f := send.Flags()
	f.String("username", "", "TURN username; a k8s:// listener brings its own")
	f.String("password", "", "TURN password; a k8s:// listener brings its own")
	f.Bool("insecure", false, "skip the verification of the TURN server's certificate")
	f.Bool("tcp", false, "relay over an RFC 6062 TCP connection to the peer")
	f.String("requested-address-family", "", `the family of the relay to request: "ipv4" requests none `+
		`and leaves it to the TURN server, "ipv6" requests IPv6; default: the family of the client's socket`)

	cmd.AddCommand(send)
	return cmd
}
