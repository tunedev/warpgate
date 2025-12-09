package cluster

import (
	"errors"
	"sync"
)

type roundRobin struct {
	mu        sync.Mutex
	name      string
	endpoints []*Endpoint
	idx       int
}

func NewRoundRobinCluster(name string, endpoints []*Endpoint) Cluster {
	return &roundRobin{
		name:      name,
		endpoints: endpoints,
	}
}

func (c *roundRobin) Name() string {
	return c.name
}

func (c *roundRobin) PickEndpoint() (*Endpoint, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	n := len(c.endpoints)
	if n == 0 {
		return nil, errors.New("cluster has no endpoints")
	}

	for i := 0; i < n; i++ {
		ep := c.endpoints[c.idx]
		c.idx = (c.idx + 1) % n
		if ep.Alive {
			return ep, nil
		}
	}

	return nil, errors.New("cluster has no alive endpoints")
}
