package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	cdsclient "github.com/l7mp/stunner/pkg/config/client"
)

func runLicense(_ *cobra.Command, args []string) error {
	jsonQuery := cdsclient.NewJSONPath()
	if ok, err := jsonQuery.Parse(output); err != nil {
		return err
	} else if ok {
		output = "jsonpath"
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
	licenseClient, err := cdsclient.NewLicenseStatusClient(pod.Addr, loggerFactory.NewLogger("cds-client"))
	if err != nil {
		return fmt.Errorf("error creating CDS client: %w", err)
	}

	status, err := licenseClient.LicenseStatus(ctx)
	if err != nil {
		return err
	}

	switch output {
	case "yaml":
		if out, err := yaml.Marshal(status); err != nil {
			return err
		} else {
			fmt.Print(string(out))
		}

	case "json":
		if out, err := json.Marshal(status); err != nil {
			return err
		} else {
			fmt.Println(string(out))
		}

	case "jsonpath":
		res, err := jsonQuery.Evaluate(status)
		if err != nil {
			return err
		}
		fmt.Println(res)

	case "summary":
		fmt.Print(status.Summary())

	default:
		fmt.Println(status.String())
	}

	return nil
}
