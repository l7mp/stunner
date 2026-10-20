package main

import (
	"cmp"
	"fmt"
	"net"
	"strconv"
	"strings"

	sipgo "github.com/emiago/sipgo/sip"
	"github.com/spf13/cobra"

	"github.com/l7mp/stunner/v2/pkg/mediactl/k8s"
	"github.com/l7mp/stunner/v2/pkg/mediactl/sip"
)

// sipCmd is the SIP phone: call dials, streams G.711-sized packets over RTP and measures the
// echo; answer answers every call and echoes it.
func (a *app) sipCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "sip", Short: "SIP calls with RTP media over UDP"}
	f := cmd.PersistentFlags()
	f.String("username", "", "digest username to answer challenges with")
	f.String("password", "", "digest password to answer challenges with")

	call := &cobra.Command{
		Use:   "call sip:<user>@<host>[:<port>]|k8s://<namespace>/<gateway>[/<listener>]",
		Short: "Dial a call, stream packets over its RTP media and measure the echo",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := a.context(cmd)
			defer cancel()
			uri := args[0]
			if k8s.IsAddress(uri) {
				addr, err := a.address(ctx, uri)
				if err != nil {
					return err
				}
				uri = fmt.Sprintf("sip:%s@%s", a.v.GetString("to"), addr)
			}
			var target sipgo.Uri
			if err := sipgo.ParseUri(uri, &target); err != nil || target.Scheme != "sip" {
				return fmt.Errorf("invalid SIP URI %q: only sip: is supported", uri)
			}
			if t, ok := target.UriParams.Get("transport"); ok && !strings.EqualFold(t, "udp") {
				return fmt.Errorf("invalid SIP URI %q: only UDP is supported", uri)
			}
			p, err := sip.New(sip.Config{Username: a.v.GetString("username"),
				Password: a.v.GetString("password"), LoggerFactory: a.lf})
			if err != nil {
				return err
			}
			defer p.Close() //nolint:errcheck
			c := a.config()
			c.Echo = true
			return a.send(ctx, p, "udp", uri, c)
		},
	}
	call.Flags().String("to", "echo", "the user part of the SIP URI of a k8s:// address")

	answer := &cobra.Command{
		Use:   "answer [<ip>][:<port>]",
		Short: "Answer every call and echo its media",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// a bare number is the port, the default port is 5060
			addr := ""
			if len(args) == 1 {
				addr = args[0]
			}
			if _, err := strconv.Atoi(addr); err == nil || addr == "" {
				addr = ":" + cmp.Or(addr, "5060")
			} else if _, _, err := net.SplitHostPort(addr); err != nil {
				addr = net.JoinHostPort(addr, "5060")
			}
			p, err := sip.New(sip.Config{Addr: addr, Username: a.v.GetString("username"),
				Password: a.v.GetString("password"), Registrar: a.v.GetBool("registrar"),
				LoggerFactory: a.lf})
			if err != nil {
				return err
			}
			ctx, cancel := a.context(cmd)
			defer cancel()
			go func() {
				for r := range p.Registered() {
					a.log.Infof("registered %s at %s for %s", r.AOR.String(), r.Contact.String(), r.Expires)
				}
			}()
			a.log.Infof("answering calls at %s", p.Addr())
			return a.serve(ctx, p, true)
		},
	}
	answer.Flags().Bool("registrar", false, "also take REGISTERs as a registrar")

	cmd.AddCommand(call, answer)
	return cmd
}
