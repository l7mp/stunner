package quota

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestQuota(t *testing.T) {
	t.Run("zero quota", func(t *testing.T) {
		q := New()
		for range 5 {
			assert.True(t, q.CheckAndIncrement("alice", "realm", 0), "a zero quota admits every session")
		}
	})

	t.Run("enforced", func(t *testing.T) {
		q := New()
		const quota = 2
		assert.True(t, q.CheckAndIncrement("alice", "realm", quota), "1st session under quota")
		assert.True(t, q.CheckAndIncrement("alice", "realm", quota), "2nd session at quota")
		assert.False(t, q.CheckAndIncrement("alice", "realm", quota), "3rd session over quota refused")

		q.Decrement("alice", "realm")
		assert.True(t, q.CheckAndIncrement("alice", "realm", quota), "a freed slot admits again")
		assert.False(t, q.CheckAndIncrement("alice", "realm", quota), "back over quota")

		assert.True(t, q.CheckAndIncrement("bob", "realm", quota), "other users are counted apart")
	})

	t.Run("decrement unknown", func(t *testing.T) {
		assert.NotPanics(t, func() { New().Decrement("ghost", "realm") })
	})
}
