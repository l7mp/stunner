// Command softphone is a minimal SIP phone: given a sip: URI it dials and sends silence, given a
// sip-l: address it answers every call and echoes it.
package main

import (
	"cmp"
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	flag "github.com/spf13/pflag"

	"github.com/l7mp/stunner/v2/pkg/logger"
	"github.com/l7mp/stunner/v2/pkg/softphone"
)

func main() {
	username := flag.String("username", "", "digest username to answer challenges with")
	password := flag.String("password", "", "digest password to answer challenges with")
	timeout := flag.Duration("timeout", 0, "hang up and exit after this long; 0 runs until interrupted")
	registrar := flag.Bool("registrar", false, "when answering, also take REGISTERs as a registrar")
	level := flag.StringP("log", "l", "all:INFO", "log level")
	format := flag.String("log-format", "text", `log format: "text" or "json"`)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: softphone [options] sip:<user>@<host>[:<port>]   dial and send silence\n"+
			"       softphone [options] sip-l:[<ip>][:<port>]         answer and echo\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(1)
	}

	lf := logger.NewLoggerFactory(*level)
	if *format == "json" {
		lf = logger.NewJSONLoggerFactory(*level)
	}
	log := lf.NewLogger("softphone")
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	arg, cfg := flag.Arg(0), softphone.Config{Username: *username, Password: *password,
		Registrar: *registrar, LoggerFactory: lf}
	if hostport, ok := strings.CutPrefix(arg, "sip-l:"); ok {
		// sip-l:[ip][:port], a bare number being the port
		if _, err := strconv.Atoi(hostport); err == nil || hostport == "" {
			hostport = ":" + cmp.Or(hostport, "5060")
		} else if _, _, err := net.SplitHostPort(hostport); err != nil {
			hostport = net.JoinHostPort(hostport, "5060")
		}
		cfg.Addr = hostport
	} else {
		var target sip.Uri
		if err := sip.ParseUri(arg, &target); err != nil || target.Scheme != "sip" {
			log.Errorf("invalid SIP URI %q: only sip: is supported", arg)
			os.Exit(1)
		}
		if t, ok := target.UriParams.Get("transport"); ok && !strings.EqualFold(t, "udp") {
			log.Errorf("invalid SIP URI %q: only UDP is supported", arg)
			os.Exit(1)
		}
	}
	if *timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, *timeout)
		defer cancel()
	}

	p, err := softphone.New(cfg)
	if err != nil {
		log.Errorf("cannot start: %s", err.Error())
		os.Exit(1)
	}
	defer p.Close() //nolint:errcheck

	if strings.HasPrefix(arg, "sip-l:") {
		log.Infof("answering calls at sip-l:%s", p.Addr())
		for {
			select {
			case call := <-p.Incoming():
				log.Infof("answered %s", call)
				go func() {
					<-call.Done()
					log.Infof("%s ended (%s): %s", call, call.EndReason(), call.Stats())
				}()
			case r := <-p.Registered():
				log.Infof("registered %s at %s for %s", r.AOR.String(), r.Contact.String(), r.Expires)
			case <-ctx.Done():
				return
			}
		}
	}

	call, err := p.Dial(ctx, arg)
	if err != nil {
		log.Errorf("call failed: %s", err.Error())
		os.Exit(1)
	}
	log.Infof("connected %s", call)
	select {
	case <-call.Done():
	case <-ctx.Done():
		hctx, hcancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = call.Hangup(hctx)
		hcancel()
	}
	log.Infof("%s ended (%s): %s", call, call.EndReason(), call.Stats())
}
