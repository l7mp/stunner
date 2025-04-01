package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	cdsclient "github.com/l7mp/stunner/pkg/config/client"
)

func runConfig(_ *cobra.Command, args []string) error {
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
		cds, err = cdsclient.NewConfigNamespaceNameAPI(pod.Addr, gwNs, gwName, "", cdslog)
	}

	if err != nil {
		return fmt.Errorf("error creating CDS client: %w", err)
	}

	confChan := make(chan *stnrv1.StunnerConfig, 8)
	if watch {
		err := cds.Watch(ctx, confChan, false)
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
				fmt.Print(string(out))
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
