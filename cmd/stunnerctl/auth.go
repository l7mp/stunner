package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/spf13/cobra"

	cdsclient "github.com/l7mp/stunner/pkg/config/client"
)

func runAuth(_ *cobra.Command, args []string) error {
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
		if username != "" {
			q.Set("username", username)
		}
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
