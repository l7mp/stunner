package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/spf13/cobra"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	"github.com/l7mp/stunner/v2/pkg/icetester"
	"github.com/l7mp/stunner/v2/pkg/logger"
)

func runICETest(_ *cobra.Command, args []string) error {
	ns := ""
	if k8sConfigFlags.Namespace != nil {
		ns = *k8sConfigFlags.Namespace
	}

	turnTransports := []stnrv2.Protocol{}
	protos := args
	if len(protos) == 0 {
		// run all tests for all transports if no specific transport is provided
		protos = []string{"udp", "tcp"}
	}
	for _, arg := range protos {
		proto, err := stnrv2.NewProtocol(arg)
		if err != nil {
			return err
		}
		switch proto {
		case stnrv2.ProtocolUDP:
			turnTransports = append(turnTransports, stnrv2.ProtocolTURNUDP)
		case stnrv2.ProtocolTCP:
			turnTransports = append(turnTransports, stnrv2.ProtocolTURNTCP)
		case stnrv2.ProtocolTURNUDP, stnrv2.ProtocolTURNTCP:
			turnTransports = append(turnTransports, proto)
		default:
			return fmt.Errorf("ICE test is currently not available on TURN transport protocol %s", proto)
		}
	}

	offloadEngine, err := stnrv2.NewOffloadEngine(iceTesterOffloadEngine)
	if err != nil {
		return fmt.Errorf("could not initialize offload engine: %w", err)
	}

	// Create a buffered logger for the tests
	logBuffer := &bytes.Buffer{}
	bufferedLoggerFactory := logger.NewLoggerFactory("all:TRACE") // hardcode highest loglevel
	bufferedLoggerFactory.SetWriter(logBuffer)

	eventCh := make(chan icetester.Event)
	tester := icetester.New(icetester.Config{
		EventChannel: eventCh,

		K8sConfigFlags:  k8sConfigFlags,
		CDSConfigFlags:  cdsConfigFlags,
		AuthConfigFlags: authConfigFlags,

		Namespace:      ns,
		TURNTransports: turnTransports,
		Image:          iceTesterImage,
		ForceCleanup:   forceCleanup,
		PacketRate:     iceTesterPacketRate,
		OffloadEngine:  offloadEngine,
		AllowNodePort:  allowNodePort,

		Logger: bufferedLoggerFactory,
	})

	// run for at most the timeout, and stop on interrupt
	ctx, cancel := context.WithTimeout(context.Background(), iceTesterTimeout)
	defer cancel()
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	// the printer drains the events until the test ends
	var wg sync.WaitGroup
	wg.Go(func() {
		for e := range eventCh {
			printEvent(e, logBuffer)
		}
	})
	err = tester.Run(ctx)
	close(eventCh)
	wg.Wait()

	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		fmt.Printf("\nICE tester timed out after %s\n", iceTesterTimeout)
		printLogs(logBuffer)
		err = fmt.Errorf("ICE test timed out after %s", iceTesterTimeout)
	case errors.Is(ctx.Err(), context.Canceled):
		fmt.Printf("\nICE tester stopped due to user interrupt\n")
		err = errors.New("ICE test interrupted")
	case err != nil:
		fmt.Printf("\nICE tester error: %s\n", err.Error())
	}

	// A non-nil error makes stunnerctl exit non-zero, so scripts can rely on the exit status.
	return err
}

func printEvent(e icetester.Event, logbuf io.ReadWriter) {
	// started
	if e.InProgress {
		over := ""
		if e.Transport != "" {
			over = " over " + e.Transport
		}
		fmt.Printf("%s: %s%s... ", e.Timestamp.Format(time.RFC822), e.Type.String(), over)
		return
	}

	// completed successfully
	if e.Error == nil {
		fmt.Println("completed")
		if e.Result != nil {
			fmt.Printf("\tStatistics: %s\n", e.Result.String())
		}
		for _, cs := range []struct {
			name  string
			cands []icetester.Candidate
		}{{"LocalICECandidates", e.Local}, {"RemoteICECandidates", e.Remote}} {
			if len(cs.cands) == 0 {
				continue
			}
			fmt.Printf("\t%s:\n", cs.name)
			for _, c := range cs.cands {
				sel := "  "
				if c.Selected {
					sel = "* "
				}
				fmt.Printf("\t  %s%s\n", sel, c.Candidate)
			}
		}
		return
	}

	// completed with error
	fmt.Printf("error\nError: %q\nTimeStamp: %s\n", e.Error.Error(),
		e.Timestamp.Format(time.DateTime))
	if e.Diagnostics != "" {
		fmt.Printf("Diagnostics: %s\n", e.Diagnostics)
	}
	printLogs(logbuf)
}

func printLogs(logbuf io.ReadWriter) {
	logs, err := io.ReadAll(logbuf)
	if err != nil {
		fmt.Printf("Logs not available due to error when reading log buffer: %s", err.Error())
	} else if len(logs) != 0 {
		fmt.Println("Detailed logs")
		fmt.Print(string(logs))
	}
}
