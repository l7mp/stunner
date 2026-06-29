package offload

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNullOffload executes Null offload unit tests.
func TestNullOffload(t *testing.T) {
	nullEngine := NewNullEngine()
	assert.NotNil(t, nullEngine, "cannot instantiate Null offload engine")
	defer nullEngine.Close() //nolint:errcheck

	assert.NoError(t, nullEngine.Start("none", nil), "cannot init Null offload engine")
}
