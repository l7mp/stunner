// Package object implements the STUNner dataplane objects (Admin, Auth, Listener, Cluster,
// etc.) and the default object catalog declaration. The reconciler walk
// (internal/reconciler) materializes and converges the tree purely from the catalog.
package object

import (
	"fmt"

	"github.com/l7mp/stunner/internal/runtime"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

type KindSpec = runtime.KindSpec
type Catalog = runtime.Catalog

// NewCatalog builds the default object catalog:
//
//	Stunner (root, singleton)
//	├── Admin ── Health / Metrics / Offload    (singletons)
//	├── Auth                                   (singleton)
//	├── Listener [N from config]
//	│   └── ListenerServer                     (lifecycle-only, owns the TURN server)
//	└── Cluster [N from config]
//	    └── Relay [one per cluster protocol]   (lifecycle-only, owns the udp/tcp relay)
func NewCatalog() *Catalog {
	specs := make([]KindSpec, 0, 10)

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
		ExtractConfigs: func(_ runtime.Runnable, full *stnrv1.StunnerConfig) ([]stnrv1.Config, error) {
			return []stnrv1.Config{full}, nil
		},
		Singleton:     true,
		SingletonName: func(_ runtime.Runnable) string { return stnrv1.DefaultStunnerName },
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
		ExtractConfigs: func(_ runtime.Runnable, full *stnrv1.StunnerConfig) ([]stnrv1.Config, error) {
			cp := full.Admin
			return []stnrv1.Config{&cp}, nil
		},
		Singleton:     true,
		SingletonName: func(_ runtime.Runnable) string { return stnrv1.DefaultAdminName },
	})

	register(KindSpec{
		Type: runtime.TypeAuth,
		New: func(_ runtime.Runnable, conf stnrv1.Config, rt *runtime.Runtime) (runtime.Runnable, error) {
			return NewAuth(conf, rt)
		},
		ExtractConfigs: func(_ runtime.Runnable, full *stnrv1.StunnerConfig) ([]stnrv1.Config, error) {
			cp := full.Auth
			return []stnrv1.Config{&cp}, nil
		},
		Singleton:     true,
		SingletonName: func(_ runtime.Runnable) string { return stnrv1.DefaultAuthName },
	})

	register(KindSpec{
		Type: runtime.TypeHealth,
		New: func(_ runtime.Runnable, conf stnrv1.Config, rt *runtime.Runtime) (runtime.Runnable, error) {
			return NewHealth(conf, rt)
		},
		ExtractConfigs: func(_ runtime.Runnable, full *stnrv1.StunnerConfig) ([]stnrv1.Config, error) {
			endpoint := defaultHealthEndpoint()
			if full.Admin.HealthCheckEndpoint != nil {
				endpoint = *full.Admin.HealthCheckEndpoint
			}
			return []stnrv1.Config{&HealthConfig{Endpoint: endpoint}}, nil
		},
		Singleton:     true,
		SingletonName: func(_ runtime.Runnable) string { return stnrv1.DefaultHealthName },
	})

	register(KindSpec{
		Type: runtime.TypeMetrics,
		New: func(_ runtime.Runnable, conf stnrv1.Config, rt *runtime.Runtime) (runtime.Runnable, error) {
			return NewMetrics(conf, rt)
		},
		ExtractConfigs: func(_ runtime.Runnable, full *stnrv1.StunnerConfig) ([]stnrv1.Config, error) {
			return []stnrv1.Config{&MetricsConfig{Endpoint: full.Admin.MetricsEndpoint}}, nil
		},
		Singleton:     true,
		SingletonName: func(_ runtime.Runnable) string { return stnrv1.DefaultMetricsName },
	})

	register(KindSpec{
		Type: runtime.TypeOffload,
		New: func(_ runtime.Runnable, conf stnrv1.Config, rt *runtime.Runtime) (runtime.Runnable, error) {
			return NewOffload(conf, rt)
		},
		ExtractConfigs: func(_ runtime.Runnable, full *stnrv1.StunnerConfig) ([]stnrv1.Config, error) {
			return []stnrv1.Config{&OffloadConfig{
				Engine:     full.Admin.OffloadEngine,
				Interfaces: append([]string(nil), full.Admin.OffloadInterfaces...),
			}}, nil
		},
		Singleton:     true,
		SingletonName: func(_ runtime.Runnable) string { return stnrv1.DefaultOffloadName },
	})

	register(KindSpec{
		Type:     runtime.TypeListener,
		Children: []runtime.ObjectType{runtime.TypeListenerServer},
		New: func(_ runtime.Runnable, conf stnrv1.Config, rt *runtime.Runtime) (runtime.Runnable, error) {
			return NewListener(conf, rt)
		},
		ExtractConfigs: func(_ runtime.Runnable, full *stnrv1.StunnerConfig) ([]stnrv1.Config, error) {
			out := make([]stnrv1.Config, len(full.Listeners))
			for i := range full.Listeners {
				lc := full.Listeners[i]
				out[i] = &lc
			}
			return out, nil
		},
	})

	register(KindSpec{
		Type: runtime.TypeListenerServer,
		New: func(parent runtime.Runnable, _ stnrv1.Config, rt *runtime.Runtime) (runtime.Runnable, error) {
			listener, ok := parent.(*Listener)
			if !ok {
				return nil, fmt.Errorf("catalog: listener-server parent is not a listener: %T", parent)
			}
			return NewListenerServer(listener, rt), nil
		},
		ExtractConfigs: func(parent runtime.Runnable, _ *stnrv1.StunnerConfig) ([]stnrv1.Config, error) {
			return []stnrv1.Config{&runtime.NodeConfig{Name: parent.Name()}}, nil
		},
		Singleton:     true,
		SingletonName: func(parent runtime.Runnable) string { return parent.Name() },
	})

	register(KindSpec{
		Type:     runtime.TypeCluster,
		Children: []runtime.ObjectType{runtime.TypeRelay},
		New: func(_ runtime.Runnable, conf stnrv1.Config, rt *runtime.Runtime) (runtime.Runnable, error) {
			return NewCluster(conf, rt)
		},
		ExtractConfigs: func(_ runtime.Runnable, full *stnrv1.StunnerConfig) ([]stnrv1.Config, error) {
			out := make([]stnrv1.Config, len(full.Clusters))
			for i := range full.Clusters {
				cc := full.Clusters[i]
				out[i] = &cc
			}
			return out, nil
		},
	})

	register(KindSpec{
		Type: runtime.TypeRelay,
		New: func(parent runtime.Runnable, conf stnrv1.Config, _ *runtime.Runtime) (runtime.Runnable, error) {
			cluster, ok := parent.(*Cluster)
			if !ok {
				return nil, fmt.Errorf("catalog: relay parent is not a cluster: %T", parent)
			}
			rc, ok := conf.(*RelayConfig)
			if !ok {
				return nil, stnrv1.ErrInvalidConf
			}
			return NewRelayNode(cluster, rc.Protocol), nil
		},
		// One relay node per cluster protocol, derived from live parent state: when a
		// cluster gains a protocol, the reconciler creates and starts only the new relay
		// without bouncing the running ones.
		ExtractConfigs: func(parent runtime.Runnable, _ *stnrv1.StunnerConfig) ([]stnrv1.Config, error) {
			cluster, ok := parent.(*Cluster)
			if !ok {
				return nil, fmt.Errorf("catalog: relay parent is not a cluster: %T", parent)
			}
			protos := cluster.Protocols()
			out := make([]stnrv1.Config, 0, len(protos))
			for _, proto := range protos {
				out = append(out, &RelayConfig{Cluster: cluster.Name(), Protocol: proto})
			}
			return out, nil
		},
	})

	return runtime.NewCatalogFromKinds(specs...)
}

// NewCatalogFromKinds builds a catalog from explicit kind specs. Used by reconciler tests.
func NewCatalogFromKinds(specs ...KindSpec) *Catalog {
	return runtime.NewCatalogFromKinds(specs...)
}
