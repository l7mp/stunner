package resolver

import (
	// "fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/l7mp/stunner/pkg/logger"
)

// var resolverTestLoglevel string = "all:TRACE"
var resolverTestLoglevel string = "all:ERROR"

func TestMockResolver(t *testing.T) {
	loggerFactory := logger.NewLoggerFactory(resolverTestLoglevel)
	log := loggerFactory.NewLogger("resolver-test")

	log.Debug("Setting up the mock DNS")
	mockDns := NewMockResolver(map[string]([]string){
		"stunner.l7mp.io":     []string{"1.2.3.4"},
		"echo-server.l7mp.io": []string{"1.2.3.5"},
		"dummy.l7mp.io":       []string{"1.2.3.10", "1.2.3.11"},
	}, loggerFactory)

	// should never err
	mockDns.Start()
	assert.NoError(t, nil, "start mock DNS")

	// should never err
	err := mockDns.Register("dummy")
	assert.NoError(t, err, "register")
	assert.NoError(t, nil, "register")

	ip, err := mockDns.Lookup("stunner.l7mp.io")
	assert.NoError(t, err, "lookup 1")
	assert.Len(t, ip, 1, "lookup ip len 1")
	assert.Equal(t, "1.2.3.4", ip[0].String(), "ip 1")

	ip, err = mockDns.Lookup("echo-server.l7mp.io")
	assert.NoError(t, err, "lookup 2")
	assert.Len(t, ip, 1, "lookup ip len 2")
	assert.Equal(t, "1.2.3.5", ip[0].String(), "ip 2")

	ip, err = mockDns.Lookup("dummy.l7mp.io")
	assert.NoError(t, err, "lookup 3")
	assert.Len(t, ip, 2, "lookup ip len 3")
	assert.Equal(t, "1.2.3.10", ip[0].String(), "ip 3")
	assert.Equal(t, "1.2.3.11", ip[1].String(), "ip 3")

	ip, err = mockDns.Lookup("nonexistent.example.com")
	assert.Error(t, err, "nonexistent lookup")
	assert.Len(t, ip, 0, "nonexistent ip")

	// should never err
	mockDns.Unregister("dummy")
	assert.NoError(t, nil, "unregister")

	// should never err
	mockDns.Close()
	assert.NoError(t, nil, "stop mock DNS")
}
