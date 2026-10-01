// Package object implements the STUNner dataplane objects (Admin, Auth, Cluster, Server,
// Listener, Health, Metrics, Offload) and the declarative object hierarchy (the catalog).
package object

import (
	"github.com/l7mp/stunner/v2/internal/reconciler"
	"github.com/l7mp/stunner/v2/internal/runtime"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

type KindSpec = reconciler.KindSpec
type Catalog = reconciler.Catalog

// NewCatalog builds the default object catalog:
//
//	Stunner (root, singleton)
//	+-- Admin -- Health / Metrics / Offload    (singletons)
//	+-- Auth                                   (singleton)
//	+-- Cluster [N from config]
//	+-- Server [N from config]
//	+-- Listener [N from config]
//
// The dataplane objects reference each other by name only, resolved through the runtime when a
// conn needs them, so each reconciles on its own: no object holds another, and none is restarted
// because another one is. Siblings start in this order, so the listeners, which hand conns to
// servers, come up last.
func NewCatalog() *Catalog {
	specs := make([]KindSpec, 0, 8)

	register := func(spec KindSpec) {
		specs = append(specs, spec)
	}

	register(KindSpec{
		Type: runtime.TypeStunner,
		Children: []runtime.ObjectType{
			runtime.TypeAdmin,
			runtime.TypeAuth,
			runtime.TypeCluster,
			runtime.TypeServer,
			runtime.TypeListener,
		},
		New: func(_ runtime.Runnable, conf stnrv2.Config, rt *runtime.Runtime) (runtime.Runnable, error) {
			return NewStunner(conf, rt)
		},
		ExtractConfigs: func(_ string, full *stnrv2.StunnerConfig) ([]stnrv2.Config, error) {
			return []stnrv2.Config{full}, nil
		},
		Singleton:     true,
		SingletonName: func(_ string) string { return stnrv2.DefaultStunnerName },
	})

	register(KindSpec{
		Type: runtime.TypeAdmin,
		Children: []runtime.ObjectType{
			runtime.TypeHealth,
			runtime.TypeMetrics,
			runtime.TypeOffload,
		},
		New: func(_ runtime.Runnable, conf stnrv2.Config, rt *runtime.Runtime) (runtime.Runnable, error) {
			return NewAdmin(conf, rt)
		},
		ExtractConfigs: func(_ string, full *stnrv2.StunnerConfig) ([]stnrv2.Config, error) {
			cp := full.Admin
			return []stnrv2.Config{&cp}, nil
		},
		Singleton:     true,
		SingletonName: func(_ string) string { return stnrv2.DefaultAdminName },
	})

	register(KindSpec{
		Type: runtime.TypeAuth,
		New: func(_ runtime.Runnable, conf stnrv2.Config, rt *runtime.Runtime) (runtime.Runnable, error) {
			return NewAuth(conf, rt)
		},
		ExtractConfigs: func(_ string, full *stnrv2.StunnerConfig) ([]stnrv2.Config, error) {
			cp := full.Auth
			return []stnrv2.Config{&cp}, nil
		},
		Singleton:     true,
		SingletonName: func(_ string) string { return stnrv2.DefaultAuthName },
	})

	register(KindSpec{
		Type: runtime.TypeHealth,
		New: func(_ runtime.Runnable, conf stnrv2.Config, rt *runtime.Runtime) (runtime.Runnable, error) {
			return NewHealth(conf, rt)
		},
		ExtractConfigs: func(_ string, full *stnrv2.StunnerConfig) ([]stnrv2.Config, error) {
			endpoint := defaultHealthEndpoint()
			if full.Admin.HealthCheckEndpoint != nil {
				endpoint = *full.Admin.HealthCheckEndpoint
			}
			return []stnrv2.Config{&HealthConfig{Endpoint: endpoint}}, nil
		},
		Singleton:     true,
		SingletonName: func(_ string) string { return stnrv2.DefaultHealthName },
	})

	register(KindSpec{
		Type: runtime.TypeMetrics,
		New: func(_ runtime.Runnable, conf stnrv2.Config, rt *runtime.Runtime) (runtime.Runnable, error) {
			return NewMetrics(conf, rt)
		},
		ExtractConfigs: func(_ string, full *stnrv2.StunnerConfig) ([]stnrv2.Config, error) {
			return []stnrv2.Config{&MetricsConfig{Endpoint: full.Admin.MetricsEndpoint}}, nil
		},
		Singleton:     true,
		SingletonName: func(_ string) string { return stnrv2.DefaultMetricsName },
	})

	register(KindSpec{
		Type: runtime.TypeOffload,
		New: func(_ runtime.Runnable, conf stnrv2.Config, rt *runtime.Runtime) (runtime.Runnable, error) {
			return NewOffload(conf, rt)
		},
		ExtractConfigs: func(_ string, full *stnrv2.StunnerConfig) ([]stnrv2.Config, error) {
			return []stnrv2.Config{&OffloadConfig{
				Engine:     full.Admin.OffloadEngine,
				Interfaces: append([]string(nil), full.Admin.OffloadInterfaces...),
			}}, nil
		},
		Singleton:     true,
		SingletonName: func(_ string) string { return stnrv2.DefaultOffloadName },
	})

	register(KindSpec{
		Type: runtime.TypeCluster,
		New: func(_ runtime.Runnable, conf stnrv2.Config, rt *runtime.Runtime) (runtime.Runnable, error) {
			return NewCluster(conf, rt)
		},
		ExtractConfigs: func(_ string, full *stnrv2.StunnerConfig) ([]stnrv2.Config, error) {
			out := make([]stnrv2.Config, len(full.Clusters))
			for i := range full.Clusters {
				cc := full.Clusters[i]
				out[i] = &cc
			}
			return out, nil
		},
	})

	register(KindSpec{
		Type: runtime.TypeServer,
		New: func(_ runtime.Runnable, conf stnrv2.Config, rt *runtime.Runtime) (runtime.Runnable, error) {
			return NewServer(conf, rt)
		},
		ExtractConfigs: func(_ string, full *stnrv2.StunnerConfig) ([]stnrv2.Config, error) {
			out := make([]stnrv2.Config, len(full.Servers))
			for i := range full.Servers {
				sc := full.Servers[i]
				out[i] = &sc
			}
			return out, nil
		},
	})

	register(KindSpec{
		Type: runtime.TypeListener,
		New: func(_ runtime.Runnable, conf stnrv2.Config, rt *runtime.Runtime) (runtime.Runnable, error) {
			return NewListener(conf, rt)
		},
		ExtractConfigs: func(_ string, full *stnrv2.StunnerConfig) ([]stnrv2.Config, error) {
			out := make([]stnrv2.Config, len(full.Listeners))
			for i := range full.Listeners {
				lc := full.Listeners[i]
				out[i] = &lc
			}
			return out, nil
		},
	})

	return reconciler.NewCatalogFromKinds(specs...)
}
