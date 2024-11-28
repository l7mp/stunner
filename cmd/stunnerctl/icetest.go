package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/l7mp/stunner/internal/icetester"
	v1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/l7mp/stunner/pkg/logger"
)

const DefaultTestNamespace = "icetest"

func runICETest(_ *cobra.Command, args []string) error {
	ns := DefaultTestNamespace
	if k8sConfigFlags.Namespace != nil && *k8sConfigFlags.Namespace != "" {
		ns = *k8sConfigFlags.Namespace
	}

	turnTransports := []v1.ListenerProtocol{}
	protos := args
	if len(protos) == 0 {
		// run all tests for all transports if no specific transport is provided
		protos = []string{"udp", "tcp"}
	}
	for _, arg := range protos {
		proto, err := v1.NewListenerProtocol(arg)
		if err != nil {
			return err
		}
		switch proto {
		case v1.ListenerProtocolUDP:
			turnTransports = append(turnTransports, v1.ListenerProtocolTURNUDP)
		case v1.ListenerProtocolTCP:
			turnTransports = append(turnTransports, v1.ListenerProtocolTURNTCP)
		case v1.ListenerProtocolTURNUDP, v1.ListenerProtocolTURNTCP:
			turnTransports = append(turnTransports, proto)
		default:
			return fmt.Errorf("ICE test is currently not available on TURN transport protocol %s", proto)
		}
	}

	// Create a buffered logger for the tests
	logBuffer := &bytes.Buffer{}
	bufferedLoggerFactory := logger.NewLoggerFactory("all:TRACE") // hardcode highest loglevel
	bufferedLoggerFactory.SetWriter(logBuffer)

	eventCh := make(chan icetester.Event, 12)
	defer close(eventCh)
	tester, err := icetester.NewICETester(icetester.Config{
		EventChannel: eventCh,

		K8sConfigFlags:  k8sConfigFlags,
		CDSConfigFlags:  cdsConfigFlags,
		AuthConfigFlags: authConfigFlags,

		Namespace:      ns,
		TURNTransports: turnTransports,
		ICETesterImage: iceTesterImage,
		ForceCleanup:   forceCleanup,
		PacketRate:     iceTesterPacketRate,

		Logger: bufferedLoggerFactory,
	})
	if err != nil {
		return fmt.Errorf("Failed to create ICE tester: %w", err)
	}

	// run for at most 5 minutes
	ctx, cancel := context.WithTimeout(context.Background(), iceTesterTimeout)

	// stop on interrupt as well
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	// event handler
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case e := <-eventCh:
				printEvent(e, logBuffer)

			case <-ctx.Done():
				return
			}
		}
	}()

	err = tester.Start(ctx)
	if err == nil {
		err = ctx.Err()
	}

	if err != nil {
		switch ctx.Err() {
		case context.DeadlineExceeded:
			fmt.Printf("\nICE tester timed out after %s\n", iceTesterTimeout)
			printLogs(logBuffer)
		case context.Canceled:
			fmt.Printf("\nICE tester stopped due to user interrupt\n")
		default:
			fmt.Printf("\nICE tester error: %s\n", err.Error())
		}
	}

	cancel()

	// wait until the printer finishes
	wg.Wait()

	return nil
}

func printEvent(e icetester.Event, logbuf io.ReadWriter) {
	// started
	if e.InProgress {
		proto := ""
		if arg, ok := e.Args["ICETransport"]; ok {
			if p, ok := arg.(string); ok {
				proto = fmt.Sprintf(" over %s", p)
			}
		}

		fmt.Printf("%s: %s%s... ", e.Timestamp.Format(time.RFC822), e.Type.String(), proto)
		return
	}

	// completed successfully
	if e.Error == nil {
		fmt.Println("completed")
		if arg, ok := e.Args["Stats"]; ok {
			if s, ok := arg.(*icetester.Stats); ok {
				fmt.Printf("\tStatistics: rate=%0.2fpps, loss=%d/%dpkts=%0.2f%%, "+
					"RTT:mean=%0.2fms/median=%0.2fms/P95=%0.2fms/P99=%0.2fms\n",
					s.SendRate, s.PacketsSent-s.PacketsReceived, s.PacketsSent,
					s.LossRate, s.MeanLatency, s.MedianLatency, s.P95Latency,
					s.P99Latency)
			}
		}

		for _, ctype := range []string{"LocalICECandidates", "RemoteICECandidates"} {
			if arg, ok := e.Args[ctype]; ok {
				if cs, ok := arg.([]icetester.CandidateDesc); ok {
					fmt.Printf("\t%s:\n", ctype)
					for _, c := range cs {
						sel := "  "
						if c.Selected {
							sel = "* "
						}
						fmt.Printf("\t  %s%s\n", sel, c.Candidate)
					}
				}
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
