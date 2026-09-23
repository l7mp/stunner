// Package object implements the STUNner dataplane objects (Admin, Auth, Listener, Cluster, etc.)
// and the declarative object hierarchy (the catalog).
package object

import (
	"github.com/l7mp/stunner/v2/internal/reconciler"
	"github.com/l7mp/stunner/v2/internal/runtime"
	stnrv1 "github.com/l7mp/stunner/v2/pkg/apis/v1"
)

type KindSpec = reconciler.KindSpec
type Catalog = reconciler.Catalog

// NewCatalog builds the default object catalog:
//
//	Stunner (root, singleton)
//	+-- Admin -- Health / Metrics / Offload    (singletons)
//	+-- Auth                                   (singleton)
//	+-- Listener [N from config]               (owns its Server)
//	+-- Cluster [N from config]
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
			runtime.TypeListener,
			runtime.TypeCluster,
		},
		New: func(_ runtime.Runnable, conf stnrv1.Config, rt *runtime.Runtime) (runtime.Runnable, error) {
			return NewStunner(conf, rt)
		},
		ExtractConfigs: func(_ string, full *stnrv1.StunnerConfig) ([]stnrv1.Config, error) {
			return []stnrv1.Config{full}, nil
		},
		Singleton:     true,
		SingletonName: func(_ string) string { return stnrv1.DefaultStunnerName },
	})

	register(KindSpec{
		Type: runtime.TypeAdmin,
		Children: []runtime.ObjectType{
			runtime.TypeHealth,
			runtime.TypeMetrics,
			runtime.TypeOffload,
		},
		New: func(_ runtime.Runnable, conf stnrv1.Config, rt *runtime.Runtime) (runtime.Runnable, error) {
			return NewAdmin(conf, rt)
		},
		ExtractConfigs: func(_ string, full *stnrv1.StunnerConfig) ([]stnrv1.Config, error) {
			cp := full.Admin
			return []stnrv1.Config{&cp}, nil
		},
		Singleton:     true,
		SingletonName: func(_ string) string { return stnrv1.DefaultAdminName },
	})

	register(KindSpec{
		Type: runtime.TypeAuth,
		New: func(_ runtime.Runnable, conf stnrv1.Config, rt *runtime.Runtime) (runtime.Runnable, error) {
			return NewAuth(conf, rt)
		},
		ExtractConfigs: func(_ string, full *stnrv1.StunnerConfig) ([]stnrv1.Config, error) {
			cp := full.Auth
			return []stnrv1.Config{&cp}, nil
		},
		Singleton:     true,
		SingletonName: func(_ string) string { return stnrv1.DefaultAuthName },
	})

	register(KindSpec{
		Type: runtime.TypeHealth,
		New: func(_ runtime.Runnable, conf stnrv1.Config, rt *runtime.Runtime) (runtime.Runnable, error) {
			return NewHealth(conf, rt)
		},
		ExtractConfigs: func(_ string, full *stnrv1.StunnerConfig) ([]stnrv1.Config, error) {
			endpoint := defaultHealthEndpoint()
			if full.Admin.HealthCheckEndpoint != nil {
				endpoint = *full.Admin.HealthCheckEndpoint
			}
			return []stnrv1.Config{&HealthConfig{Endpoint: endpoint}}, nil
		},
		Singleton:     true,
		SingletonName: func(_ string) string { return stnrv1.DefaultHealthName },
	})

	register(KindSpec{
		Type: runtime.TypeMetrics,
		New: func(_ runtime.Runnable, conf stnrv1.Config, rt *runtime.Runtime) (runtime.Runnable, error) {
			return NewMetrics(conf, rt)
		},
		ExtractConfigs: func(_ string, full *stnrv1.StunnerConfig) ([]stnrv1.Config, error) {
			return []stnrv1.Config{&MetricsConfig{Endpoint: full.Admin.MetricsEndpoint}}, nil
		},
		Singleton:     true,
		SingletonName: func(_ string) string { return stnrv1.DefaultMetricsName },
	})

	register(KindSpec{
		Type: runtime.TypeOffload,
		New: func(_ runtime.Runnable, conf stnrv1.Config, rt *runtime.Runtime) (runtime.Runnable, error) {
			return NewOffload(conf, rt)
		},
		ExtractConfigs: func(_ string, full *stnrv1.StunnerConfig) ([]stnrv1.Config, error) {
			return []stnrv1.Config{&OffloadConfig{
				Engine:     full.Admin.OffloadEngine,
				Interfaces: append([]string(nil), full.Admin.OffloadInterfaces...),
			}}, nil
		},
		Singleton:     true,
		SingletonName: func(_ string) string { return stnrv1.DefaultOffloadName },
	})

	register(KindSpec{
		Type: runtime.TypeListener,
		New: func(_ runtime.Runnable, conf stnrv1.Config, rt *runtime.Runtime) (runtime.Runnable, error) {
			return NewListener(conf, rt)
		},
		ExtractConfigs: func(_ string, full *stnrv1.StunnerConfig) ([]stnrv1.Config, error) {
			out := make([]stnrv1.Config, len(full.Listeners))
			for i := range full.Listeners {
				lc := full.Listeners[i]
				out[i] = &lc
			}
			return out, nil
		},
	})

	register(KindSpec{
		Type: runtime.TypeCluster,
		New: func(_ runtime.Runnable, conf stnrv1.Config, rt *runtime.Runtime) (runtime.Runnable, error) {
			return NewCluster(conf, rt)
		},
		ExtractConfigs: func(_ string, full *stnrv1.StunnerConfig) ([]stnrv1.Config, error) {
			out := make([]stnrv1.Config, len(full.Clusters))
			for i := range full.Clusters {
				cc := full.Clusters[i]
				out[i] = &cc
			}
			return out, nil
		},
	})

	return reconciler.NewCatalogFromKinds(specs...)
}
