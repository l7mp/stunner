package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	v1 "github.com/l7mp/stunner/pkg/apis/v1"
	cdsclient "github.com/l7mp/stunner/pkg/config/client"
)

func runStatus(_ *cobra.Command, args []string) error {
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

		s := v1.StunnerStatus{}
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
				fmt.Print(string(out))
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
		case "string":
			if pod.Proxy {
				fmt.Printf("%s/%s:\n\t%s\n", pod.Namespace, pod.Name, s.String())
			} else {
				fmt.Printf("%s:\n\t%s\n", pod.Addr, s.String())
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
