package main

import (
	"context"
	"fmt"

	"github.com/l7mp/stunner/v2/pkg/utils/discovery"

	"github.com/spf13/cobra"
)

func runAuth(_ *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.Debug("searching for authentication server")
	pod, err := discovery.DiscoverK8sAuthServer(ctx, k8sConfigFlags, authConfigFlags,
		loggerFactory.NewLogger("auth-fwd"))
	if err != nil {
		return fmt.Errorf("error searching for auth service: %w", err)
	}

	q := discovery.AuthQuery{Username: username, TURNCredential: authConfigFlags.TurnAuth}
	if k8sConfigFlags.Namespace != nil {
		q.Namespace = *k8sConfigFlags.Namespace
	}
	if len(args) > 0 {
		q.Gateway = args[0]
	}
	log.Debugf("querying authentication server %s", pod.String())
	b, err := discovery.GetAuth(ctx, pod.Addr, q)
	if err != nil {
		return err
	}

	fmt.Println(string(b))

	return nil
}
