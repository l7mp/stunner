package main

import (
	"log"
	"log/slog"
	"os"

	"github.com/l7mp/stunner"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

func main() {
	// Setup JSON logging
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	
	// Redirect standard log to slog
	log.SetFlags(0)
	log.SetOutput(slog.NewLogLogger(handler, slog.LevelInfo))
	
	// Create a slog logger for any direct slog usage
	slogger := slog.New(handler)
	slogger.Info("Starting Stunner with JSON logging")
	
	// Create Stunner instance
	st := stunner.NewStunner(stunner.Options{
		Name:     "json-log-example",
		LogLevel: "all:INFO",
		DryRun:   true, // Don't actually start servers
	})
	defer st.Close()
	
	// Create a simple configuration
	config := &stnrv1.StunnerConfig{
		ApiVersion: stnrv1.ApiVersion,
		Admin: stnrv1.AdminConfig{
			LogLevel: "all:INFO",
		},
		Auth: stnrv1.AuthConfig{
			Type: "plaintext",
			Credentials: map[string]string{
				"username": "user1",
				"password": "passwd1",
			},
		},
		Listeners: []stnrv1.ListenerConfig{{
			Name:     "default-listener",
			Protocol: "udp",
			Addr:     "127.0.0.1",
			Port:     3478,
			Routes:   []string{"allow-any"},
		}},
		Clusters: []stnrv1.ClusterConfig{{
			Name:      "allow-any",
			Endpoints: []string{"0.0.0.0/0"},
		}},
	}
	
	// Reconcile the configuration
	if err := st.Reconcile(config); err != nil {
		slogger.Error("Failed to reconcile configuration", "error", err.Error())
		os.Exit(1)
	}
	
	slogger.Info("Stunner configuration applied successfully")
} 