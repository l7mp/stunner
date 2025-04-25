package main

import (
	"fmt"
	"os"
	"time"

	"github.com/pion/logging"
	"github.com/spf13/cobra"
	cliopt "k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/l7mp/stunner/internal/icetester"
	v1 "github.com/l7mp/stunner/pkg/apis/v1"
	cdsclient "github.com/l7mp/stunner/pkg/config/client"
	"github.com/l7mp/stunner/pkg/logger"
)

// list all configs: stunnerctl get config --all-namespaces
// watch all configs in namesapce stunner: stunnerctl -n stunner get config --watch
// get short-form config for stunner/udp-gateway: stunnerctl -n stunner get config udp-gateway
// get config for stunner/udp-gateway in yaml format: stunnerctl -n stunner get config udp-gateway --output yaml

var (
	output, username, loglevel                                     string
	iceTesterImage, iceTesterOffloadEngine, configRelayAddressNode string
	watch, all, verbose, forceCleanup, allowNodePort               bool
	k8sConfigFlags                                                 *cliopt.ConfigFlags
	cdsConfigFlags                                                 *cdsclient.CDSConfigFlags
	authConfigFlags                                                *cdsclient.AuthConfigFlags
	podConfigFlags                                                 *cdsclient.PodConfigFlags
	iceTesterTimeout                                               time.Duration
	iceTesterPacketRate                                            int

	loggerFactory logger.LoggerFactory
	log           logging.LeveledLogger

	rootCmd = &cobra.Command{
		Use:               "stunnerctl",
		Short:             "A command line utility to inspect STUNner dataplane .",
		Long:              "The stunnerctl tool is a CLI for inspecting, watching and troublehssooting STUNner gateways",
		DisableAutoGenTag: true,
		PersistentPreRun: func(_ *cobra.Command, _ []string) {
			if verbose {
				loglevel = "all:TRACE"
			}

			loggerFactory = logger.NewLoggerFactory(loglevel)
			log = loggerFactory.NewLogger("stunnerctl")
		},
	}
)

var (
	configCmd = &cobra.Command{
		Use:               "config",
		Aliases:           []string{"stunner-config"},
		Short:             "Get or watch dataplane configs of a gateway",
		Args:              cobra.RangeArgs(0, 1),
		DisableAutoGenTag: true,
		Run: func(cmd *cobra.Command, args []string) {
			if err := runConfig(cmd, args); err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
		},
	}
	statusCmd = &cobra.Command{
		Use:               "status [gateway]",
		Aliases:           []string{"dataplane-status"},
		Short:             "Read status from dataplane pods for a gateway",
		Args:              cobra.RangeArgs(0, 1),
		DisableAutoGenTag: true,
		Run: func(cmd *cobra.Command, args []string) {
			if err := runStatus(cmd, args); err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
		},
	}
	authCmd = &cobra.Command{
		Use:               "auth",
		Aliases:           []string{"get-credential"},
		Short:             "Obtain authenticaction credentials for a gateway",
		Args:              cobra.RangeArgs(0, 1),
		DisableAutoGenTag: true,
		Run: func(cmd *cobra.Command, args []string) {
			if err := runAuth(cmd, args); err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
		},
	}
	iceTestCmd = &cobra.Command{
		Use:               "icetest [udp, tcp, ...]",
		Short:             "Test ICE connectivity with the specified transports",
		DisableAutoGenTag: true,
		Run: func(cmd *cobra.Command, args []string) {
			if err := runICETest(cmd, args); err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
		},
	}
	licenseCmd = &cobra.Command{
		Use:               "license",
		Aliases:           []string{"license-status"},
		Short:             "Get licensing status",
		Args:              cobra.RangeArgs(0, 1),
		DisableAutoGenTag: true,
		Run: func(cmd *cobra.Command, args []string) {
			if err := runLicense(cmd, args); err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
		},
	}
)

func init() {
	rootCmd.PersistentFlags().BoolVarP(&all, "all-namespaces", "a", false, "Consider all namespaces")
	rootCmd.PersistentFlags().StringVarP(&output, "output", "o", "summary", "Output format, either json, yaml, summary or jsonpath=template")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging, identical to -l all:DEBUG (overrides -l)")
	rootCmd.PersistentFlags().StringVarP(&loglevel, "loglevel", "l", "all:WARN", "Set loglevel (format: <scope>:<level>, overrides: PION_LOG_*, default: all:WARN)")

	// Kubernetes config flags: persistent, all commands
	k8sConfigFlags = cliopt.NewConfigFlags(true)
	k8sConfigFlags.AddFlags(rootCmd.PersistentFlags())

	// CDS server discovery flags: for the "config" and "license" commands
	cdsConfigFlags = cdsclient.NewCDSConfigFlags()
	cdsConfigFlags.AddFlags(configCmd.Flags())
	cdsConfigFlags.AddFlags(licenseCmd.Flags())

	// watch flag: only for config
	configCmd.Flags().BoolVarP(&watch, "watch", "w", false, "Watch for config updates from server")
	configCmd.Flags().StringVar(&configRelayAddressNode, "node", "",
		"Perform relay address discovery (if available) with respect to the given node.")

	// Pod discovery flags: only for "status" command
	podConfigFlags = cdsclient.NewPodConfigFlags()
	podConfigFlags.AddFlags(statusCmd.Flags())

	// Auth discovery flags: only for "auth" command
	authConfigFlags = cdsclient.NewAuthConfigFlags()
	authConfigFlags.AddFlags(authCmd.Flags())
	authCmd.Flags().StringVarP(&username, "username", "u", "",
		"User id for generating an ephemeral credential (Default is empty username)")

	// ICE test: uses CDS and auth args
	cdsConfigFlags.AddFlags(iceTestCmd.Flags())
	authConfigFlags.AddFlags(iceTestCmd.Flags())

	// ICE test timeout
	iceTestCmd.Flags().IntVarP(&iceTesterPacketRate, "packet-rate", "r", 50,
		"Packet rate [pkts/sec], 0 means flood test (Default: 50)")
	iceTestCmd.Flags().DurationVarP(&iceTesterTimeout, "timeout", "t", icetester.DefaultICETesterTimeout,
		"Timeout")
	iceTestCmd.Flags().StringVar(&iceTesterImage, "ice-tester-image", icetester.DefaultICETesterImage,
		"Icetester container image")
	iceTestCmd.Flags().StringVar(&iceTesterOffloadEngine, "offload-mode", v1.OffloadEngineNone.String(),
		"TURN (UDP) offload mode")
	iceTestCmd.Flags().BoolVar(&forceCleanup, "force-cleanup", false,
		"Remove tester namespace if it exists before launching the ICE test")
	iceTestCmd.Flags().BoolVar(&allowNodePort, "allow-nodeport", false,
		"Allow connecting to STUNner via a NodePort (may require prior firewall configuration)")

	// Add commands
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(authCmd)
	rootCmd.AddCommand(iceTestCmd)
	rootCmd.AddCommand(licenseCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Whoops. There was an error while executing your CLI '%s'", err)
		os.Exit(1)
	}
}
