package util

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

type endpointTest struct {
	name, input, output, ipnet string
	port, endPort              int
	success                    bool
}

var endpointTester = []endpointTest{
	{
		name:    "ipv4 - full",
		input:   "1.2.3.4/16:<1-2>",
		output:  "1.2.0.0/16:<1-2>",
		ipnet:   "1.2.0.0/16",
		port:    1,
		endPort: 2,
		success: true,
	},
	{
		name:    "ipv4 - no port",
		input:   "1.2.3.4/16",
		output:  "1.2.0.0/16",
		ipnet:   "1.2.0.0/16",
		port:    1,
		endPort: 65535,
		success: true,
	},
	{
		name:    "ipv4 - no prefix len",
		input:   "1.2.3.4:<1-2>",
		output:  "1.2.3.4:<1-2>",
		ipnet:   "1.2.3.4/32",
		port:    1,
		endPort: 2,
		success: true,
	},
	{
		name:    "ipv4 - no port, no prefix len ",
		input:   "1.2.3.4",
		output:  "1.2.3.4",
		ipnet:   "1.2.3.4/32",
		port:    1,
		endPort: 65535,
		success: true,
	},
	{
		name:    "ipv6 - full",
		input:   "2001:db8:3333:4444:5555:6666:7777:8888/32:<1-2>",
		output:  "2001:db8::/32:<1-2>",
		ipnet:   "2001:db8::/32",
		port:    1,
		endPort: 2,
		success: true,
	},
	{
		name:    "ipv6 - no port",
		input:   "2001:db8:3333:4444:5555:6666:7777:8888/32",
		output:  "2001:db8::/32",
		ipnet:   "2001:db8::/32",
		port:    1,
		endPort: 65535,
		success: true,
	},
	{
		name:    "ipv6 - no prefix len",
		input:   "2001:db8:3333:4444:5555:6666:7777:8888:<1-2>",
		output:  "2001:db8:3333:4444:5555:6666:7777:8888:<1-2>",
		ipnet:   "2001:db8:3333:4444:5555:6666:7777:8888/128",
		port:    1,
		endPort: 2,
		success: true,
	},
	{
		name:    "ipv6 - no port, no prefix len ",
		input:   "2001:db8:3333:4444:5555:6666:7777:8888",
		output:  "2001:db8:3333:4444:5555:6666:7777:8888",
		ipnet:   "2001:db8:3333:4444:5555:6666:7777:8888/128",
		port:    1,
		endPort: 65535,
		success: true,
	},
	{
		name:    "ipv4 - no addr fails ",
		input:   ":<1-65535>",
		success: false,
	},
	{
		name:    "ipv4 - random stuff fails ",
		input:   "dummy",
		success: false,
	},
}

func TestEndpointParse(t *testing.T) {
	for _, c := range endpointTester {
		t.Run(c.name, func(t *testing.T) {
			ep, err := ParseEndpoint(c.input)
			if c.success {
				assert.NoError(t, err, "parse")
				assert.Equal(t, c.ipnet, ep.prefix.String(), "ip equal")
				assert.Equal(t, c.port, ep.port, "port equal")
				assert.Equal(t, c.endPort, ep.endPort, "endport equal")
				assert.Equal(t, c.output, ep.String(), "output")
			} else {
				assert.Error(t, err, "parse")
			}

		})
	}
}

type matchTest struct {
	name, input, ip string
	port            int
	match, route    bool
}

var matchTester = []matchTest{{
	name:  "ipv4 - full - both",
	input: "1.2.3.4/16:<1-2>",
	ip:    "1.2.3.5",
	port:  1,
	match: true,
	route: true,
}, {
	name:  "ipv4 - full - route",
	input: "1.2.3.4/16:<1-2>",
	ip:    "1.2.4.6",
	port:  3,
	match: false,
	route: true,
}, {
	name:  "ipv4 - full - neither",
	input: "1.2.3.4/16:<1-2>",
	ip:    "1.3.3.4",
	port:  1,
	match: false,
	route: false,
}}

func TestRouteMatch(t *testing.T) {
	for _, c := range matchTester {
		t.Run(c.name, func(t *testing.T) {
			ep, err := ParseEndpoint(c.input)
			assert.NoError(t, err, "endpoint parse")
			ip := net.ParseIP(c.ip)
			assert.NotNil(t, ip, "ip parse")
			assert.True(t, ep.Contains(ip) == c.route, "route")
			assert.True(t, ep.Match(ip, c.port) == c.match, "match")
		})
	}
}
