package object

import (
	licensecfg "github.com/l7mp/stunner/pkg/config/license"
)

// Router is an object that knows how to match clusters and listeners.
type Router interface {
	// GetAdmin returns the admin object. Panics if no admin object is available.
	GetAdmin() *Admin

	// GetAuth returns the authenitation object. Panics if no auth object is available.
	GetAuth() *Auth

	// GetListeners returns all STUNner listeners.
	GetListeners() []*Listener

	// GetListener returns a STUNner listener or nil of no listener with the given name was found.
	GetListener(name string) *Listener

	// GetClusters returns all STUNner clusters.
	GetClusters() []*Cluster

	// GetCluster returns a STUNner cluster or nil if no cluster with the given name was found.
	GetCluster(name string) *Cluster

	// GetLicenseConfigManager returns the license manager.
	GetLicenseConfigManager() licensecfg.ConfigManager
}

// GetClustersForListener returns the clusters for a listener.
func GetClustersForListener(l *Listener) []*Cluster {
	ret := []*Cluster{}
	for _, route := range l.Routes {
		for _, o := range l.reg.LookupAll(TypeCluster) {
			if o.ObjectName() == route {
				ret = append(ret, o.(*Cluster))
			}
		}
	}

	return ret
}
