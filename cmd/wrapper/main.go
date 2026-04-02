package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	flag "github.com/spf13/pflag"
	cliopt "k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/l7mp/stunner"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/l7mp/stunner/pkg/buildinfo"
	cdsclient "github.com/l7mp/stunner/pkg/config/client"
)

// StunnerWrapper encapsulates the Stunner main functionality with JSON logging
type StunnerWrapper struct {
	stunner        *stunner.Stunner
	jsonLogger     *slog.Logger
	originalLogger *log.Logger
	config         *stnrv1.StunnerConfig
	ctx            context.Context
	cancel         context.CancelFunc
	interceptor    *LoggerInterceptor
}

// slogWriter converts log output to slog for JSON formatting
type slogWriter struct {
	logger *slog.Logger
}

func (w *slogWriter) Write(p []byte) (n int, err error) {
	msg := strings.TrimSpace(string(p))
	w.logger.Info(msg)
	return len(p), nil
}

// NewStunnerWrapper creates a new wrapper instance
func NewStunnerWrapper() *StunnerWrapper {
	return &StunnerWrapper{}
}

// SetupJSONLogging configures JSON logging for all output
func (w *StunnerWrapper) SetupJSONLogging() {
	// Create JSON handler
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	
	// Create JSON logger
	w.jsonLogger = slog.New(handler)
	
	// Redirect standard log to JSON format
	log.SetFlags(0)
	log.SetOutput(&slogWriter{logger: w.jsonLogger})
	
	// Create and start the logger interceptor
	w.interceptor = NewLoggerInterceptor(w.jsonLogger)
	if err := w.interceptor.Start(); err != nil {
		w.jsonLogger.Error("Failed to start logger interceptor", "error", err.Error())
	}
	
	w.jsonLogger.Info("JSON logging wrapper initialized")
}

// InitializeStunner sets up the Stunner instance with JSON logging
func (w *StunnerWrapper) InitializeStunner(options stunner.Options) error {
	w.stunner = stunner.NewStunner(options)
	w.jsonLogger.Info("Stunner instance created", 
		"name", options.Name,
		"logLevel", options.LogLevel,
		"dryRun", options.DryRun)
	return nil
}

// LoadConfiguration loads and validates the Stunner configuration
func (w *StunnerWrapper) LoadConfiguration(configOrigin string, watch bool, k8sConfigFlags *cliopt.ConfigFlags, cdsConfigFlags *cdsclient.CDSConfigFlags) error {
	w.ctx, w.cancel = context.WithCancel(context.Background())
	
	if configOrigin == "k8s" {
		w.jsonLogger.Info("Discovering configuration from Kubernetes")
		cdsAddr, err := cdsclient.DiscoverK8sCDSServer(w.ctx, k8sConfigFlags, cdsConfigFlags,
			w.stunner.GetLogger().NewLogger("cds-fwd"))
		if err != nil {
			w.jsonLogger.Error("Error searching for CDS server", "error", err.Error())
			return err
		}
		configOrigin = cdsAddr.Addr
	}

	if !watch {
		w.jsonLogger.Info("Loading configuration", "origin", configOrigin)
		config, err := w.stunner.LoadConfig(configOrigin)
		if err != nil {
			w.jsonLogger.Error("Failed to load configuration", "error", err.Error())
			return err
		}
		w.config = config
		w.jsonLogger.Info("Configuration loaded successfully")
	} else {
		w.jsonLogger.Info("Starting configuration watcher", "origin", configOrigin)
		// For watch mode, we'll handle config updates in the main loop
	}
	
	return nil
}

// StartMainLoop runs the main event loop with JSON logging
func (w *StunnerWrapper) StartMainLoop(watch bool, configOrigin string, k8sConfigFlags *cliopt.ConfigFlags, cdsConfigFlags *cdsclient.CDSConfigFlags) error {
	conf := make(chan *stnrv1.StunnerConfig, 1)
	defer close(conf)

	var cancelConfigLoader context.CancelFunc

	// Handle initial configuration
	if w.config != nil {
		conf <- w.config
	} else if watch {
		w.jsonLogger.Info("Bootstrapping with minimal config")
		z := cdsclient.ZeroConfig(w.stunner.GetId())
		conf <- z

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		cancelConfigLoader = cancel

		if configOrigin == "k8s" {
			w.jsonLogger.Info("Discovering configuration from Kubernetes")
			cdsAddr, err := cdsclient.DiscoverK8sCDSServer(ctx, k8sConfigFlags, cdsConfigFlags,
				w.stunner.GetLogger().NewLogger("cds-fwd"))
			if err != nil {
				w.jsonLogger.Error("Error searching for CDS server", "error", err.Error())
				return err
			}
			configOrigin = cdsAddr.Addr
		}

		w.jsonLogger.Info("Watching configuration", "origin", configOrigin)
		if err := w.stunner.WatchConfig(ctx, configOrigin, conf, true); err != nil {
			w.jsonLogger.Error("Could not run config watcher", "error", err.Error())
			return err
		}
	}

	// Setup signal handling
	sigterm := make(chan os.Signal, 1)
	defer close(sigterm)
	signal.Notify(sigterm, syscall.SIGTERM, syscall.SIGINT)

	exit := make(chan bool, 1)
	defer close(exit)

	// Main event loop
	for {
		select {
		case <-exit:
			w.jsonLogger.Info("Normal exit on graceful shutdown")
			return nil

		case <-sigterm:
			w.jsonLogger.Info("Commencing graceful shutdown", 
				"activeConnections", w.stunner.AllocationCount())
			w.stunner.Shutdown()

			if cancelConfigLoader != nil {
				w.jsonLogger.Info("Canceling config loader")
				cancelConfigLoader()
				cancelConfigLoader = nil
			}

			go func() {
				for {
					if w.stunner.AllocationCount() == 0 {
						exit <- true
						return
					}
					time.Sleep(time.Second)
				}
			}()

		case c := <-conf:
			w.jsonLogger.Info("New configuration available", "config", c.String())

			w.jsonLogger.Debug("Initiating reconciliation")

			if err := w.stunner.Reconcile(c); err != nil {
				if e, ok := err.(stnrv1.ErrRestarted); ok {
					w.jsonLogger.Debug("Reconciliation ready", "message", e.Error())
				} else {
					w.jsonLogger.Error("Could not reconcile new configuration", 
						"error", err.Error(),
						"note", "running configuration unchanged")
				}
			}

			w.jsonLogger.Debug("Reconciliation ready")
		}
	}
}

// Close cleans up resources
func (w *StunnerWrapper) Close() {
	if w.interceptor != nil {
		w.interceptor.Stop()
	}
	if w.stunner != nil {
		w.stunner.Close()
	}
	if w.cancel != nil {
		w.cancel()
	}
	w.jsonLogger.Info("Stunner wrapper closed")
}

// RunStunnerWithJSONLogging is the main entry point that wraps the original stunnerd functionality
func RunStunnerWithJSONLogging() error {
	os.Args[0] = "stunnerd"
	
	var config = flag.StringP("config", "c", "", "Config origin, either a valid address in the format IP:port, or HTTP URL to the CDS server, or literal \"k8s\" to discover the CDS server from Kubernetes, or a proper file name URI in the format file://<path-to-config-file> (overrides: STUNNER_CONFIG_ORIGIN)")
	var level = flag.StringP("log", "l", "", "Log level (format: <scope>:<level>, overrides: PION_LOG_*, default: all:INFO)")
	var id = flag.StringP("id", "i", "", "Id for identifying with the CDS server (format: <namespace>/<name>, overrides: STUNNER_NAMESPACE/STUNNER_NAME, default: <default/stunnerd-hostname>)")
	var watch = flag.BoolP("watch", "w", false, "Watch config file for updates (default: false)")
	var udpThreadNum = flag.IntP("udp-thread-num", "u", 0, "Number of readloop threads (CPU cores) per UDP listener. Zero disables UDP multithreading (default: 0)")
	var dryRun = flag.BoolP("dry-run", "d", false, "Suppress side-effects, intended for testing (default: false)")
	var verbose = flag.BoolP("verbose", "v", false, "Verbose logging, identical to <-l all:DEBUG>")
	var jsonLog = flag.BoolP("json-log", "j", false, "Enable JSON formatted logging (default: false)")

	// Kubernetes config flags
	k8sConfigFlags := cliopt.NewConfigFlags(true)
	k8sConfigFlags.AddFlags(flag.CommandLine)

	// CDS server discovery flags
	cdsConfigFlags := cdsclient.NewCDSConfigFlags()
	cdsConfigFlags.AddFlags(flag.CommandLine)

	flag.Parse()

	// Always enable JSON logging in wrapper mode
	*jsonLog = true

	// Create wrapper instance
	wrapper := NewStunnerWrapper()
	defer wrapper.Close()

	// Setup JSON logging
	wrapper.SetupJSONLogging()

	// Parse configuration
	logLevel := stnrv1.DefaultLogLevel
	if *verbose {
		logLevel = "all:DEBUG"
	}
	if *level != "" {
		logLevel = *level
	}

	configOrigin := stnrv1.DefaultConfigDiscoveryAddress
	if origin, ok := os.LookupEnv(stnrv1.DefaultEnvVarConfigOrigin); ok {
		configOrigin = origin
	}
	if *config != "" {
		configOrigin = *config
	}

	nodeName := ""
	if node, ok := os.LookupEnv(stnrv1.DefaultEnvVarNodeName); ok {
		nodeName = node
	}

	if *id == "" {
		name, ok1 := os.LookupEnv(stnrv1.DefaultEnvVarName)
		namespace, ok2 := os.LookupEnv(stnrv1.DefaultEnvVarNamespace)
		if ok1 && ok2 {
			*id = fmt.Sprintf("%s/%s", namespace, name)
		}
	}

	// Initialize Stunner
	if err := wrapper.InitializeStunner(stunner.Options{
		Name:                 *id,
		LogLevel:             logLevel,
		DryRun:               *dryRun,
		NodeName:             nodeName,
		UDPListenerThreadNum: *udpThreadNum,
	}); err != nil {
		return err
	}

	// Log startup information
	buildInfo := buildinfo.BuildInfo{Version: "dev", CommitHash: "n/a", BuildDate: "<unknown>"}
	wrapper.jsonLogger.Info("Starting stunnerd with JSON logging wrapper", 
		"id", wrapper.stunner.GetId(),
		"buildInfo", buildInfo.String())

	// Handle default configuration case
	if flag.NArg() == 1 {
		wrapper.jsonLogger.Info("Starting with default configuration", "turnUri", flag.Arg(0))

		c, err := stunner.NewDefaultConfig(flag.Arg(0))
		if err != nil {
			wrapper.jsonLogger.Error("Could not load default STUNner config", "error", err.Error())
			return err
		}

		wrapper.config = c
	} else {
		// Load configuration
		if err := wrapper.LoadConfiguration(configOrigin, *watch, k8sConfigFlags, cdsConfigFlags); err != nil {
			return err
		}
	}

	// Start main loop
	return wrapper.StartMainLoop(*watch, configOrigin, k8sConfigFlags, cdsConfigFlags)
}

func main() {
	if err := RunStunnerWithJSONLogging(); err != nil {
		os.Exit(1)
	}
} 