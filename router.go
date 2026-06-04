package stunner

import "github.com/l7mp/stunner/internal/object"

// Router is an object that knows how to return the clusters corresponding to a listener.
type Router interface {
	Route(*object.Listener) []*object.Cluster
}

func (s *Stunner) NewRouter() Router {
	return &router{s: s}
}

type router struct {
	s *Stunner
}

func (r *router) Route(l *object.Listener) []*object.Cluster {
	r.s.listenerManager.RLock()
	defer r.s.listenerManager.RUnlock()

	ret := []*object.Cluster{}
	for _, route := range l.Routes {
		if c := r.s.GetCluster(route); c != nil {
			ret = append(ret, c)
		}
	}

	return ret
}
