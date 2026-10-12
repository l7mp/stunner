package resolver

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/pion/logging"
)

// Clusters register their domain endpoints with a DnsResolver that resolves them in the
// background.

const (
	dnsUpdateInterval = 5 * time.Second
)

type DnsResolver interface {
	Register(domain string) error
	Unregister(domain string)
	Lookup(domain string) ([]net.IP, error)
	Start()
	Close()
}

type serviceEntry struct {
	lock      sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
	refCount  int
	resolver  net.Resolver
	domain    string
	hostNames []net.IP
}

type dnsResolverImpl struct {
	ctx      context.Context
	lock     sync.Mutex // guards register
	register map[string]*serviceEntry
	log      logging.LeveledLogger
}

// NewDnsResolver creates a new DNS resolver
func NewDnsResolver(name string, logger logging.LoggerFactory) DnsResolver {
	log := logger.NewLogger(name)
	log.Tracef("newDnsResolver")

	return &dnsResolverImpl{
		ctx:      context.Background(),
		register: make(map[string]*serviceEntry),
		log:      log,
	}
}

// Register adds domain name to the resolver queue for background resolution
func (r *dnsResolverImpl) Register(domain string) error {
	r.log.Tracef("register: %q", domain)

	r.lock.Lock()
	e, found := r.register[domain]
	if found {
		e.refCount += 1
		r.lock.Unlock()
		return nil
	}

	resolverCtx, cancel := context.WithCancel(r.ctx)
	e = &serviceEntry{
		ctx:      resolverCtx,
		cancel:   cancel,
		refCount: 1,
		resolver: net.Resolver{PreferGo: true},
		domain:   domain,
	}
	r.register[domain] = e
	r.lock.Unlock()

	// the first resolution is synchronous: a cluster routes nothing before its domains resolve
	if err := doResolve(e); err != nil {
		r.log.Debugf("initial resolution failed for domain %q: %s", domain, err.Error())
	}

	r.log.Debugf("starting resolver thread for domain %q", domain)
	go startResolver(e, r.log)

	return nil
}

// the resolver goroutine
func startResolver(e *serviceEntry, log logging.LeveledLogger) {
	log.Infof("resolver thread starting for domain %q, DNS update interval: %v",
		e.domain, dnsUpdateInterval)

	ticker := time.NewTicker(dnsUpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-e.ctx.Done():
			log.Debugf("resolver thread exiting for domain %q", e.domain)
			return
		case <-ticker.C:
			log.Tracef("resolving for domain %q", e.domain)
			if err := doResolve(e); err != nil {
				log.Debugf("resolution failed for domain %q: %s",
					e.domain, err.Error())
			}
			log.Tracef("periodic resolution ready for domain %q, found %d endpoints", e.domain,
				len(e.hostNames))
		}
	}
}

// do the heavy lifting
func doResolve(e *serviceEntry) error {
	hosts, err := e.resolver.LookupHost(e.ctx, e.domain)
	if err != nil {
		return fmt.Errorf("failed to resolve domain %q: %s", e.domain, err.Error())
	}

	ips := make([]net.IP, 0, len(hosts))
	for _, h := range hosts {
		if n := net.ParseIP(h); n != nil {
			ips = append(ips, n)
		}
	}

	e.lock.Lock()
	defer e.lock.Unlock()
	e.hostNames = ips

	return nil
}

// Unregister removes a domain name from the resolver queue
func (r *dnsResolverImpl) Unregister(domain string) {
	r.log.Tracef("unregister: %q", domain)

	r.lock.Lock()
	defer r.lock.Unlock()

	e, found := r.register[domain]
	if !found {
		r.log.Tracef("trying to unregister resolver for unknown domain: %q", domain)
		return
	}

	e.refCount -= 1
	if e.refCount == 0 {
		e.cancel()
		delete(r.register, domain)
	}

	r.log.Infof("domain %q successfully unregistered", domain)
}

// Lookup returns the hostname(s) for a domain
func (r *dnsResolverImpl) Lookup(domain string) ([]net.IP, error) {
	r.log.Tracef("lookup domain: %q", domain)

	r.lock.Lock()
	e, found := r.register[domain]
	r.lock.Unlock()
	if !found {
		return []net.IP{}, fmt.Errorf("unknown domain name: %q", domain)
	}

	e.lock.RLock()
	defer e.lock.RUnlock()

	ret := make([]net.IP, len(e.hostNames))
	copy(ret, e.hostNames)

	r.log.Tracef("lookup ready: domain %q, endpoints: %d", domain, len(ret))

	return ret, nil
}

// Start is a no-op: Register spawns the background resolver threads.
func (r *dnsResolverImpl) Start() {
	r.log.Debugf("starting")
}

// Close closes the background resolver
func (r *dnsResolverImpl) Close() {
	r.lock.Lock()
	defer r.lock.Unlock()

	r.log.Debugf("closing: active domains: %d", len(r.register))
	for domain, e := range r.register {
		r.log.Debugf("stopping resolver for active domain %q, refCount: %d", domain, e.refCount)
		e.cancel()
		delete(r.register, domain)
	}
}
