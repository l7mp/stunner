package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/pion/logging"
	"github.com/spf13/cobra"
	cliopt "k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/util/jsonpath"

	"github.com/l7mp/stunner/internal/icetester"
	cdsclient "github.com/l7mp/stunner/pkg/config/client"
	"github.com/l7mp/stunner/pkg/logger"
)

// list all configs: stunnerctl get config --all-namespaces
// watch all configs in namesapce stunner: stunnerctl -n stunner get config --watch
// get short-form config for stunner/udp-gateway: stunnerctl -n stunner get config udp-gateway
// get config for stunner/udp-gateway in yaml format: stunnerctl -n stunner get config udp-gateway --output yaml

var (
	output, iceTesterImage, username, loglevel string
	watch, all, verbose, forceCleanup          bool
	k8sConfigFlags                             *cliopt.ConfigFlags
	cdsConfigFlags                             *cdsclient.CDSConfigFlags
	authConfigFlags                            *cdsclient.AuthConfigFlags
	podConfigFlags                             *cdsclient.PodConfigFlags
	iceTesterTimeout                           time.Duration
	iceTesterPacketRate                        int

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
		"Default icetester container image")
	iceTestCmd.Flags().BoolVar(&forceCleanup, "force-cleanup", false, "Remove tester namespace if it exists")

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

// ////////////////////////
var jsonRegexp = regexp.MustCompile(`^\{\.?([^{}]+)\}$|^\.?([^{}]+)$`)

// k8s.io/kubectl/pkg/cmd/get
func RelaxedJSONPathExpression(pathExpression string) (string, error) {
	if len(pathExpression) == 0 {
		return pathExpression, nil
	}
	submatches := jsonRegexp.FindStringSubmatch(pathExpression)
	if submatches == nil {
		return "", fmt.Errorf("unexpected path string, expected a 'name1.name2' or '.name1.name2' or '{name1.name2}' or '{.name1.name2}'")
	}
	if len(submatches) != 3 {
		return "", fmt.Errorf("unexpected submatch list: %v", submatches)
	}
	var fieldSpec string
	if len(submatches[1]) != 0 {
		fieldSpec = submatches[1]
	} else {
		fieldSpec = submatches[2]
	}
	return fmt.Sprintf("{.%s}", fieldSpec), nil
}

func ParseJSONPathFlag(output string) (*jsonpath.JSONPath, string, error) {
	if !strings.HasPrefix(output, "jsonpath") {
		return nil, output, nil
	}

	as := strings.Split(output, "=")
	if len(as) != 2 || as[0] != "jsonpath" {
		return nil, output, fmt.Errorf("invalid jsonpath output definition %q", output)
	}

	jsonQuery := jsonpath.New("output")

	// Parse and print jsonpath
	fields, err := RelaxedJSONPathExpression(as[1])
	if err != nil {
		return nil, output, fmt.Errorf("invalid jsonpath query %w", err)
	}

	if err := jsonQuery.Parse(fields); err != nil {
		return nil, output, fmt.Errorf("cannor parse jsonpath query %w", err)
	}

	return jsonQuery, "jsonpath", nil
}
