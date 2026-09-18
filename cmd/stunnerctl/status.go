package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/l7mp/stunner/v2/pkg/utils/discovery"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	cdsclient "github.com/l7mp/stunner/v2/pkg/config/client"
)

func runStatus(_ *cobra.Command, args []string) error {
	jsonQuery := cdsclient.NewJSONPath()
	if ok, err := jsonQuery.Parse(output); err != nil {
		return err
	} else if ok {
		output = "jsonpath"
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

	log.Debug("searching for dataplane pods " + extraLog)
	pods, err := discovery.DiscoverK8sStunnerdPods(ctx, k8sConfigFlags, podConfigFlags,
		gwNs, gw, loggerFactory.NewLogger("stunnerd-fwd"))
	if err != nil {
		return fmt.Errorf("error searching for stunnerd pods: %w", err)
	}

	for _, pod := range pods {
		status, err := discovery.GetStunnerdStatus(ctx, pod.Addr)
		if err != nil {
			log.Errorf("error querying status for stunnerd pod %s: %s", pod.String(), err.Error())
			continue
		}
		s := *status

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
			res, err := jsonQuery.Evaluate(s)
			if err != nil {
				return err
			}
			fmt.Println(res)
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
