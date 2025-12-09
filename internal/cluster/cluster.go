package cluster

import "net/url"

type Endpoint struct {
	URL   *url.URL
	Alive bool
}

type LoadBalancer interface {
	Pick() (*Endpoint, error)
}

type Cluster interface {
	Name() string
	PickEndpoint() (*Endpoint, error)
}
