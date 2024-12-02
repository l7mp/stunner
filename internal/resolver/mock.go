package resolver

import (
	"fmt"
	"net"
	"strings"

	"github.com/pion/logging"
)

// for testing
type MockResolver struct {
	Zone map[string]([]string)
	log  logging.LeveledLogger
}

// NewMockResolver creates a new mock DNS resolver
func NewMockResolver(zone map[string]([]string), logger logging.LoggerFactory) DnsResolver {
	return &MockResolver{
		Zone: zone,
		log:  logger.NewLogger("mock-dns"),
	}
}

// Starts spawns the mock DNS resolver
func (m *MockResolver) Start() {
	ds := []string{}
	for d, ips := range m.Zone {
		ds = append(ds, fmt.Sprintf("%s:[%s]", d, strings.Join(ips, ",")))
	}
	m.log.Debugf("Starting mock DNS resolver with zone: %s", strings.Join(ds, ", "))
}

// Close closes the mock resolver
func (m *MockResolver) Close() {
	m.log.Debugf("Closing mock DNS resolver")
}

// Register mocks the DNS resolver's Register method
func (m *MockResolver) Register(domain string) error {
	m.log.Tracef("Register (mock): %q", domain)
	return nil
}

// Unregister mocks the Unregister method
func (m *MockResolver) Unregister(domain string) {
	m.log.Tracef("Unregister (mock): %q", domain)
}

// Lookup returns the hostname(s) for a domain
func (m *MockResolver) Lookup(domain string) ([]net.IP, error) {
	m.log.Tracef("Lookup domain in mock DNS: %q", domain)

	for d := range m.Zone {
		if d == domain {
			if e, found := m.Zone[domain]; found {
				ret := []net.IP{}
				for _, i := range e {
					ret = append(ret, net.ParseIP(i))
				}
				return ret, nil
			}
		}
	}

	return []net.IP{}, fmt.Errorf("host %q not found: 3(NXDOMAIN)", domain)
}
