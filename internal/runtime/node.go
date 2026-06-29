package runtime

import (
	"fmt"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// NodeConfig is a name-only config used by lifecycle-only (Runnable) kinds: the reconciler
// needs configs only for set-membership diffing, and lifecycle-only nodes derive all real
// state from their parent.
type NodeConfig struct {
	Name string `json:"name"`
}

func (c *NodeConfig) Validate() error    { return nil }
func (c *NodeConfig) ConfigName() string { return c.Name }
func (c *NodeConfig) DeepEqual(other stnrv1.Config) bool {
	o, ok := other.(*NodeConfig)
	return ok && c.Name == o.Name
}
func (c *NodeConfig) DeepCopyInto(dst stnrv1.Config) {
	if d, ok := dst.(*NodeConfig); ok {
		*d = *c
	}
}
func (c *NodeConfig) String() string { return fmt.Sprintf("NodeConfig{name=%q}", c.Name) }
