package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

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
	k8sConfigFlags      *cliopt.ConfigFlags
	cdsConfigFlags      *cdsclient.CDSConfigFlags
	authConfigFlags     *cdsclient.AuthConfigFlags
	podConfigFlags      *cdsclient.PodConfigFlags
	loggerFactory       *logger.LeveledLoggerFactory
	log                 logging.LeveledLogger

	rootCmd = &cobra.Command{
		Use:               "stunnerctl",
		Short:             "A command line utility to inspect STUNner dataplane .",
		Long:              "The stunnerctl tool is a CLI for inspecting, watching and troublehssooting STUNner gateways",
		DisableAutoGenTag: true,
	}
)

var (
	configCmd = &cobra.Command{
		Use:               "config",
		Aliases:           []string{"stunner-config"},
		Short:             "Get or watch dataplane configs",
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
		Use:               "status",
		Aliases:           []string{"dataplane-status"},
		Short:             "Read status from dataplane pods",
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
		Short:             "Obtain authenticaction credentials to a gateway",
		Args:              cobra.RangeArgs(0, 1),
		DisableAutoGenTag: true,
		Run: func(cmd *cobra.Command, args []string) {
			if err := runAuth(cmd, args); err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
		},
	}
)

func init() {
	rootCmd.PersistentFlags().BoolVarP(&all, "all-namespaces", "a", false, "Consider all namespaces")
	rootCmd.PersistentFlags().StringVarP(&output, "output", "o", "summary", "Output format, either json, yaml, summary or jsonpath=template")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging, identical to -l all:DEBUG")

	// Kubernetes config flags: persistent, all commands
	k8sConfigFlags = cliopt.NewConfigFlags(true)
	k8sConfigFlags.AddFlags(rootCmd.PersistentFlags())

	// CDS server discovery flags: only for "config" command
	cdsConfigFlags = cdsclient.NewCDSConfigFlags()
	cdsConfigFlags.AddFlags(configCmd.Flags())

	// watch flag: only for config
	configCmd.Flags().BoolVarP(&watch, "watch", "w", false, "Watch for config updates from server")

	// Pod discovery flags: only for "status" command
	podConfigFlags = cdsclient.NewPodConfigFlags()
	podConfigFlags.AddFlags(statusCmd.Flags())

	// Auth discovery flags: only for "auth" command
	authConfigFlags = cdsclient.NewAuthConfigFlags()
	authConfigFlags.AddFlags(authCmd.Flags())

	// Add commands
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(authCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Whoops. There was an error while executing your CLI '%s'", err)
		os.Exit(1)
	}
}

func runConfig(_ *cobra.Command, args []string) error {
	loglevel := "all:WARN"
	if verbose {
		loglevel = "all:TRACE"
	}
	loggerFactory = logger.NewLoggerFactory(loglevel)
	log = loggerFactory.NewLogger("stunnerctl")

	gwNs := "default"
	if k8sConfigFlags.Namespace != nil && *k8sConfigFlags.Namespace != "" {
		gwNs = *k8sConfigFlags.Namespace
	}

	jsonQuery, output, err := ParseJSONPathFlag(output)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.Debug("Searching for CDS server")
	pod, err := cdsclient.DiscoverK8sCDSServer(ctx, k8sConfigFlags, cdsConfigFlags,
		loggerFactory.NewLogger("cds-fwd"))
	if err != nil {
		return fmt.Errorf("error searching for CDS server: %w", err)
	}

	log.Debugf("Connecting to CDS server: %s", pod.String())
	var cds cdsclient.CdsApi
	cdslog := loggerFactory.NewLogger("cds-client")
	if all {
		cds, err = cdsclient.NewAllConfigsAPI(pod.Addr, cdslog)
	} else if len(args) == 0 {
		cds, err = cdsclient.NewConfigsNamespaceAPI(pod.Addr, gwNs, cdslog)
	} else {
		gwName := args[0]
		cds, err = cdsclient.NewConfigNamespaceNameAPI(pod.Addr, gwNs, gwName, cdslog)
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
			fmt.Print(c.Summary())
		default:
			fmt.Println(c.String())
		}
	}

	return nil
}

func runStatus(_ *cobra.Command, args []string) error {
	loglevel := "all:WARN"
	if verbose {
		loglevel = "all:TRACE"
	}
	loggerFactory = logger.NewLoggerFactory(loglevel)
	log = loggerFactory.NewLogger("stunnerctl")

	jsonQuery, output, err := ParseJSONPathFlag(output)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gwNs := "default"
	extraLog := "in namespace default"
	if k8sConfigFlags.Namespace != nil && *k8sConfigFlags.Namespace != "" {
		gwNs = *k8sConfigFlags.Namespace
		extraLog = fmt.Sprintf("in namespace %s", gwNs)
	}
	// --all-namespaces overrides -n
	if all {
		gwNs = ""
		extraLog = "in all namespaces"
	}

	gw := ""
	if len(args) > 0 {
		gw = args[0]
	}
	if gwNs != "" && gw != "" {
		extraLog += fmt.Sprintf("for gateway %s", gw)
	}

	log.Debug("Searching for dataplane pods " + extraLog)
	pods, err := cdsclient.DiscoverK8sStunnerdPods(ctx, k8sConfigFlags, podConfigFlags,
		gwNs, gw, loggerFactory.NewLogger("stunnerd-fwd"))
	if err != nil {
		return fmt.Errorf("error searching for stunnerd pods: %w", err)
	}

	for _, pod := range pods {
		client := http.Client{
			Timeout: 5 * time.Second,
		}
		url := fmt.Sprintf("http://%s/status", pod.Addr)
		res, err := client.Get(url)
		if err != nil {
			log.Errorf("Error querying status for stunnerd pod at URL %q on %s: %s",
				url, pod.String(), err.Error())
			continue
		}

		if res.StatusCode != http.StatusOK {
			log.Errorf("Status query failed on %s with HTTP error code %s",
				pod.String(), res.Status)
			continue
		}

		s := stnrv1.StunnerStatus{}
		err = json.NewDecoder(res.Body).Decode(&s)
		if err != nil {
			log.Errorf("Could not decode status response: %s", err.Error())
			continue
		}

		switch output {
		case "yaml":
			if out, err := yaml.Marshal(s); err != nil {
				return err
			} else {
				fmt.Println(string(out))
			}
		case "json":
			if out, err := json.Marshal(s); err != nil {
				return err
			} else {
				fmt.Println(string(out))
			}
		case "jsonpath":
			values, err := jsonQuery.FindResults(s)
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
			fallthrough
		default:
			if pod.Proxy {
				fmt.Printf("%s/%s:\n\t%s\n", pod.Namespace, pod.Name, s.Summary())
			} else {
				fmt.Printf("%s:\n\t%s\n", pod.Addr, s.Summary())
			}
		}
	}

	return nil
}

func runAuth(cmd *cobra.Command, args []string) error {
	loglevel := "all:WARN"
	if verbose {
		loglevel = "all:TRACE"
	}
	loggerFactory = logger.NewLoggerFactory(loglevel)
	log = loggerFactory.NewLogger("stunnerctl")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.Debug("Searching for authentication server")
	pod, err := cdsclient.DiscoverK8sAuthServer(ctx, k8sConfigFlags, authConfigFlags,
		loggerFactory.NewLogger("auth-fwd"))
	if err != nil {
		return fmt.Errorf("error searching for auth service: %w", err)
	}

	u := url.URL{
		Scheme: "http",
		Host:   pod.Addr,
		Path:   "/ice",
	}
	q := u.Query()
	q.Set("service", "turn")
	u.RawQuery = q.Encode()

	if authConfigFlags.TurnAuth {
		// enforce TURN credential format
		u.Path = ""
	}

	if k8sConfigFlags.Namespace != nil && *k8sConfigFlags.Namespace != "" {
		q := u.Query()
		q.Set("namespace", *k8sConfigFlags.Namespace)
		if len(args) > 0 {
			q.Set("gateway", args[0])
		}
		u.RawQuery = q.Encode()
	}

	log.Debugf("Querying to authentication server %s using URL %q", pod.String(), u.String())
	res, err := http.Get(u.String())
	if err != nil {
		return fmt.Errorf("error querying auth service %s: %w", pod.String(), err)
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP error querying auth service %s: expected status %d, got %d",
			pod.String(), http.StatusOK, res.StatusCode)
	}

	b, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("cannot read HTTP response: %w", err)
	}

	fmt.Println(string(b))

	return nil
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
