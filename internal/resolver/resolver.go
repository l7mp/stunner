package resolver

import (
	"fmt"
	"net"
        "context"
        "time"
        "sync"

	"github.com/pion/logging"
)

// STRICT_DNS clusters embed a DnsResolver to resolve domain names in the background

const (
        dnsUpdateInterval = 5 * time.Second
)

type DnsResolver interface {
        Register(domain string) error
        Unregister(domain string)
        Lookup(domain string) ([]net.IP, error)
        Start()
        Close()
        SetResolver(mockDns *MockResolver)
}

type serviceEntry struct {
        lock sync.RWMutex
        ctx context.Context
        cancel context.CancelFunc
        refCount int
        resolver net.Resolver
        domain string
        hostNames []net.IP
        cname string
        lastResolved time.Time
        mockResolver *MockResolver
}

type dnsResolverImpl struct {
        ctx context.Context
        register map[string]*serviceEntry
        log logging.LeveledLogger
        mockResolver *MockResolver
}

// NewDnsResolver creates a new DNS resolver
func NewDnsResolver(name string, logger logging.LoggerFactory) DnsResolver {
        log := logger.NewLogger(name)
        log.Tracef("NewDnsResolver")

        return &dnsResolverImpl{
                ctx: context.Background(),
                register: make(map[string]*serviceEntry),
                log: log,
                mockResolver: nil,
        }
}

// SetResolver overrides the default DNS resolver with a mock resolver for testing
func (r *dnsResolverImpl) SetResolver(mockDns *MockResolver) {
        r.log.Debugf("SetResolver: using mock resolver: %#v", *mockDns)

        r.mockResolver = mockDns

        for h, e := range r.register {
                e.lock.Lock()
                e.mockResolver = mockDns
                e.lock.Unlock()
                r.register[h] = e
        }
}

// Register adds domain name to the resolver queue for background resolution
func (r *dnsResolverImpl) Register(domain string) error {
        r.log.Tracef("Register: %q", domain)

        e, found := r.register[domain]
        if found {
                e.refCount += 1
                return nil
        }

        resolverCtx, cancel := context.WithCancel(r.ctx)
        e = &serviceEntry{
		lock: sync.RWMutex{},
                ctx: resolverCtx,
                cancel: cancel,
                refCount: 1,
                resolver: net.Resolver{PreferGo: true},
                domain: domain,
                cname: "",
                lastResolved: time.Time{},
                mockResolver: r.mockResolver,
        }
        r.register[domain] = e        

        r.log.Debugf("Starting resolver thread for domain %q", domain)
        go startResolver(e, r.log)
        
        return nil
}

// the resolver goroutine
func startResolver(e *serviceEntry, log logging.LeveledLogger) {
        log.Infof("resolver thread starting for domain %q, DNS update interval: %v",
                e.domain, dnsUpdateInterval)

        if err := doResolve(e); err != nil {
                log.Debugf("initial resolution failed for domain %q: %s", e.domain, err.Error())
        }
        log.Tracef("initial resolution ready for domain %q, found %d endpoints", e.domain,
                len(e.hostNames))

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
        if e.cname == "" {
                // lookupCNAME is a wrapper (see below) for testing with the MockResolver
                cname, err := lookupCNAME(e, e.domain)
                if err != nil {
                        return fmt.Errorf("Cannot resolve CNAME for domain %q: %s",
                                e.domain, err.Error())
                }
                e.cname = cname
        }

        // lookupHost is a wrapper (see below) for testing with the MockResolver
        hosts, err := lookupHost(e, e.domain)
                if err != nil {
                        return fmt.Errorf("Cannot resolve CNAME for domain %q: %s",
                                e.domain, err.Error())
                }

        e.lastResolved = time.Now()

        // for writing
        e.lock.Lock()
        defer e.lock.Unlock()

        e.hostNames = make([]net.IP, len(hosts))
        for i, h := range hosts {
                n := net.ParseIP(h)
                if n == nil {
                        // skip silently
                        continue
                }
                e.hostNames[i] = n
        }

        return nil
}

// Unregister removes a domain name from the resolver queue
func (r *dnsResolverImpl) Unregister(domain string) {
        r.log.Tracef("Unregister: %q", domain)

        e, found := r.register[domain]
        if !found {
                r.log.Tracef("trying to ungregister resolver for unknown domain: %q", domain)
                return
        }

        e.refCount -= 1
        if e.refCount == 0 {
                e.cancel()
                delete(r.register, domain)
        }

        r.log.Infof("domain %q succesfully unregistered", domain)
}

// Lookup returns the hostname(s) for a domain
func (r *dnsResolverImpl) Lookup(domain string) ([]net.IP, error) {
        r.log.Tracef("Lookup domain: %q", domain)

        e, found := r.register[domain]
        if !found {
                return []net.IP{}, fmt.Errorf("Unknown domain name: %q", domain)
        }

        e.lock.RLock()
        defer e.lock.RUnlock()

        ret := make([]net.IP, len(e.hostNames))
        for i, n := range e.hostNames {
                ret[i] = n
        }

        return ret, nil
}

// Starts spawns the background resolver thread
func (r *dnsResolverImpl) Start() {
        r.log.Debugf("Starting")
        // Register already started the resolver threads
}

// Close closes the background resolver
func (r *dnsResolverImpl) Close() {
        r.log.Debugf("Closing: active domains: %d", len(r.register))
        // XXX: if the server Close sequence is OK then this should never happen
        if len(r.register) > 0 {
                r.log.Warnf("trying to close DNS resolver with %d active domains",
                        len(r.register))
                for _, e := range r.register {
                        r.log.Debugf("domain %q is active, refCount: %d",
                                e.domain, e.refCount)
                }
        }
}

// for testing
type MockResolver struct {
        Zone map[string]([]string)
}

func (m *MockResolver) LookupCNAME(domain string) (string, error) {
        for d, _ := range m.Zone {
                if d == domain {
                        return domain, nil
                }
        }

        return "", fmt.Errorf("Host %q not found: 3(NXDOMAIN)", domain)
}

func (m *MockResolver) LookupHost(domain string) ([]string, error) {
        if e, found := m.Zone[domain]; found != false {
                return e, nil
        }

        return []string{}, fmt.Errorf("Host %q not found: 3(NXDOMAIN)", domain)
}

// lookupCNAME is a wrapper (see below) for testing with the MockResolver
func lookupCNAME(e *serviceEntry, domain string) (string, error) {
        if e.mockResolver != nil {
                return e.mockResolver.LookupCNAME(domain)
        }

        return e.resolver.LookupCNAME(e.ctx, domain)
}

// lookupHost is a wrapper (see below) for testing with the MockResolver
func lookupHost(e *serviceEntry, domain string) ([]string, error) {
        if e.mockResolver != nil {
                return e.mockResolver.LookupHost(domain)
        }

        return e.resolver.LookupHost(e.ctx, domain)
}
