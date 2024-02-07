package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"

	"github.com/pion/logging"
	"github.com/spf13/cobra"
	cliopt "k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/util/jsonpath"
	"sigs.k8s.io/yaml"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	cdsclient "github.com/l7mp/stunner/pkg/config/client"
	"github.com/l7mp/stunner/pkg/logger"
)

// list all configs: stunnerctl get config --all-namespaces
// watch all configs in namesapce stunner: stunnerctl -n stunner get config --watch
// get short-form config for stunner/udp-gateway: stunnerctl -n stunner get config udp-gateway
// get config for stunner/udp-gateway in yaml format: stunnerctl -n stunner get config udp-gateway --output yaml

var (
	output              string
	watch, all, verbose bool
	jsonQuery           *jsonpath.JSONPath
	k8sConfigFlags      *cliopt.ConfigFlags
	cdsConfigFlags      *cdsclient.CDSConfigFlags
	loggerFactory       *logger.LeveledLoggerFactory
	log                 logging.LeveledLogger

	rootCmd = &cobra.Command{
		Use:               "stunnerctl",
		Short:             "A command line utility to inspect STUNner dataplane configurations.",
		Long:              "The stunnerctl tool is a CLI for inspecting, watching and troublehssooting the configuration of STUNner gateways",
		DisableAutoGenTag: true,
	}
)

var (
	configCmd = &cobra.Command{
		Use:               "config",
		Aliases:           []string{"stunner-config"},
		Short:             "Gets or watches STUNner configs",
		Args:              cobra.RangeArgs(0, 1),
		DisableAutoGenTag: true,
		Run: func(cmd *cobra.Command, args []string) {
			if err := runConfig(cmd, args); err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
		},
	}
)

func init() {
	rootCmd.PersistentFlags().BoolVarP(&watch, "watch", "w", false, "Watch for config updates from server")
	rootCmd.PersistentFlags().BoolVarP(&all, "all-namespaces", "a", false, "Consider all namespaces")
	rootCmd.PersistentFlags().StringVarP(&output, "output", "o", "summary", "Output format")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging, identical to -l all:DEBUG")

	// Kubernetes config flags
	k8sConfigFlags = cliopt.NewConfigFlags(true)
	k8sConfigFlags.AddFlags(rootCmd.PersistentFlags())

	// CDS server discovery flags
	cdsConfigFlags = cdsclient.NewCDSConfigFlags()
	cdsConfigFlags.AddFlags(rootCmd.PersistentFlags())

	rootCmd.AddCommand(configCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Whoops. There was an error while executing your CLI '%s'", err)
		os.Exit(1)
	}
}

func runConfig(cmd *cobra.Command, args []string) error {
	loglevel := "all:WARN"
	if verbose {
		loglevel = "all:TRACE"
	}
	loggerFactory = logger.NewLoggerFactory(loglevel)
	log = loggerFactory.NewLogger("stunnerctl")

	gwNs := "default"
	if k8sConfigFlags.Namespace != nil {
		gwNs = *k8sConfigFlags.Namespace
	}

	if strings.HasPrefix(output, "jsonpath") {
		as := strings.Split(output, "=")
		if len(as) != 2 || as[0] != "jsonpath" {
			return fmt.Errorf("invalid jsonpath output definition %q", output)
		}

		jsonQuery = jsonpath.New("output")

		// Parse and print jsonpath
		fields, err := RelaxedJSONPathExpression(as[1])
		if err != nil {
			return fmt.Errorf("invalid jsonpath query %w", err)
		}

		if err := jsonQuery.Parse(fields); err != nil {
			return fmt.Errorf("cannor parse jsonpath query %w", err)
		}
		output = "jsonpath"
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.Debug("Searching for CDS server")
	cdsAddr, err := cdsclient.DiscoverK8sCDSServer(ctx, k8sConfigFlags, cdsConfigFlags,
		loggerFactory.NewLogger("cds-fwd"))
	if err != nil {
		return fmt.Errorf("error searching for CDS server: %w", err)
	}

	var cds cdsclient.CdsApi
	cdslog := loggerFactory.NewLogger("cds-client")
	if all {
		cds, err = cdsclient.NewAllConfigsAPI(cdsAddr, cdslog)
	} else if len(args) == 0 {
		cds, err = cdsclient.NewConfigsNamespaceAPI(cdsAddr, gwNs, cdslog)
	} else {
		gwName := args[0]
		cds, err = cdsclient.NewConfigNamespaceNameAPI(cdsAddr, gwNs, gwName, cdslog)
	}

	if err != nil {
		return fmt.Errorf("error creating CDS client: %w", err)
	}

	confChan := make(chan *stnrv1.StunnerConfig, 8)
	if watch {
		err := cds.Watch(ctx, confChan)
		if err != nil {
			close(confChan)
			return err
		}

		go func() {
			sigs := make(chan os.Signal, 1)
			signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
			<-sigs
			close(confChan)
		}()
	} else {
		resp, err := cds.Get(ctx)
		if err != nil {
			close(confChan)
			return err
		}
		for _, c := range resp {
			confChan <- c
		}

		close(confChan)
	}

	for c := range confChan {
		if cdsclient.IsConfigDeleted(c) {
			fmt.Printf("Gateway: %s <deleted>\n", c.Admin.Name)
			continue
		}
		switch output {
		case "yaml":
			if out, err := yaml.Marshal(c); err != nil {
				return err
			} else {
				fmt.Println(string(out))
			}
		case "json":
			if out, err := json.Marshal(c); err != nil {
				return err
			} else {
				fmt.Println(string(out))
			}
		case "jsonpath":
			values, err := jsonQuery.FindResults(c)
			if err != nil {
				return err
			}

			if len(values) == 0 || len(values[0]) == 0 {
				fmt.Println("<none>")
			}

			for arrIx := range values {
				for valIx := range values[arrIx] {
					fmt.Printf("%v\n", values[arrIx][valIx].Interface())
				}
			}
		case "summary":
			fmt.Print(string(c.Summary()))
		case "status":
			fallthrough
		default:
			fmt.Println(c.String())
		}
	}

	return nil
}

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
