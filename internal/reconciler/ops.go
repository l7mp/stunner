package reconciler

import (
	"github.com/l7mp/stunner/internal/runtime"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// createPlan is a deferred construction: the plan walk records what to build (kind, config, and
// parent identity), and the construct phase calls New parents-first once the walk is complete.
type createPlan struct {
	typ        runtime.ObjectType
	config     stnrv1.Config
	parentType runtime.ObjectType
	parentName string
}

// constructedRef is an object built by the construct phase, awaiting registration. Registration
// is deferred until after reconcile so a reconcile failure leaves the registry untouched.
type constructedRef struct {
	obj    runtime.Runnable
	parent runtime.Runnable
}

type reconcileRef struct {
	Object runtime.Object
	Config stnrv1.Config
}

type ops struct {
	reconcile      []reconcileRef
	create         []createPlan
	delete         []runtime.Runnable
	start          []runtime.Runnable
	stop           []runtime.Runnable
	restartedNames []string
	startSet       map[string]bool
	stopSet        map[string]bool
	deleteSet      map[string]bool
}

func newOps() *ops {
	return &ops{
		startSet:  map[string]bool{},
		stopSet:   map[string]bool{},
		deleteSet: map[string]bool{},
	}
}

func opKey(o runtime.Runnable) string { return string(o.Type()) + "/" + o.Name() }

func restartedErrorFromOps(ops *ops) error {
	if len(ops.restartedNames) == 0 {
		return nil
	}
	return stnrv1.ErrRestarted{Objects: append([]string(nil), ops.restartedNames...)}
}

func (ops *ops) addStop(o runtime.Runnable) {
	k := opKey(o)
	if ops.stopSet[k] {
		return
	}
	ops.stopSet[k] = true
	ops.stop = append(ops.stop, o)
}

func (ops *ops) addStart(o runtime.Runnable) {
	k := opKey(o)
	if ops.startSet[k] {
		return
	}
	ops.startSet[k] = true
	ops.start = append(ops.start, o)
}

func (ops *ops) addDelete(o runtime.Runnable) {
	k := opKey(o)
	if ops.deleteSet[k] {
		return
	}
	ops.deleteSet[k] = true
	ops.delete = append(ops.delete, o)
}
