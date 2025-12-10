# Configuration

Warpgate is configured using a YAML file. The default path is:

```bash
./configs/warpgate.yaml
````

or you can override via:

```bash
warpgate --config /path/to/config.yaml
```

## Top-level structure

```yaml
server:      # Legacy single-listener mode (still supported)
cache:
clusters:
routes:
listeners:   # Optional multi-listener mode
```

If `listeners` is defined, Warpgate will run one `http.Server` per listener.
If `listeners` is **omitted**, Warpgate falls back to `server.address` and `server.tls`.

---

## `server` (legacy single listener)

```yaml
server:
  address: ":8080"
  tls:
    enabled: false
    certFile: "cert.pem"
    keyFile: "key.pem"
  ipBlockCIDRs:
    - "10.0.0.0/8"
    - "192.168.1.10/32"
```

* `address` - bind address for the HTTP server.
* `tls.enabled` - if true, uses `ListenAndServeTLS`.
* `tls.certFile`, `tls.keyFile` - TLS certificate and key paths.
* `ipBlockCIDRs` - optional list of CIDR ranges; if set, the IP filter middleware will deny requests from those ranges with `403 Forbidden`.

---

## `listeners` (multi-listener mode)

```yaml
listeners:
  - name: "http"
    address: ":8080"
    tls:
      enabled: false
    redirectTo: "https"

  - name: "https"
    address: ":8443"
    tls:
      enabled: true
      certFile: "cert.pem"
      keyFile: "key.pem"
```

* `name` - logical name for this listener.
* `address` - bind address (e.g. `:8080`, `0.0.0.0:8443`).
* `tls` - same schema as `server.tls`.
* `redirectTo` - optional; if set on a **non-TLS** listener, that listener will act as an HTTP→HTTPS redirect to the target listener name.

Example behaviour:

* HTTP listener `http` on `:8080` has `redirectTo: https`
* HTTPS listener `https` on `:8443`
* A request to `http://example.com:8080/api/hello` will be redirected to `https://example.com:8443/api/hello`.

---

## `cache`

```yaml
cache:
  maxEntries: 1000
  defaultTTL: 30s
  maxBodyBytes: 1048576
```

* `maxEntries` - maximum number of cache entries in the in-memory LRU.
* `defaultTTL` - TTL used when the response does not specify a `Cache-Control: max-age=` directive.
* `maxBodyBytes` - responses larger than this size are not cached.

---

## `clusters`

```yaml
clusters:
  - name: "api_cluster"
    endpoints:
      - "http://localhost:9000"
      - "http://localhost:9001"
    healthCheck:
      path: "/health"
      interval: 5s
      timeout: 1s
      unhealthyThreshold: 3
      healthyThreshold: 1
    circuitBreaker:
      consecutiveFailures: 5
      cooldown: 30s
```

* `name` - logical name of the cluster.
* `endpoints` - list of upstream URLs (scheme, host, port).
* `healthCheck` - optional active health check configuration:

  * `path` - path to call on each endpoint (e.g. `/health`).
  * `interval` - how often to probe.
  * `timeout` - per-request timeout.
  * `unhealthyThreshold` - mark endpoint unhealthy after this many consecutive failures.
  * `healthyThreshold` - mark endpoint healthy after this many consecutive successes.
* `circuitBreaker` - optional per-endpoint circuit breaker:

  * `consecutiveFailures` - number of request-level failures before opening the circuit.
  * `cooldown` - how long to keep the circuit open before trying again.

---

## `routes`

```yaml
routes:
  - name: "api"
    pathPrefix: "/api"
    cluster: "api_cluster"
    cache:
      enabled: true
      ttl: 10s
```

* `name` - route name (used mainly for clarity and metrics labelling).
* `pathPrefix` - incoming path prefix to match (e.g. `/api`).
* `cluster` - name of the target cluster for this route.
* `cache` - optional per-route cache override:

  * `enabled` - whether to enable caching for this route.
  * `ttl` - optional per-route TTL; if zero, falls back to `cache.defaultTTL` or `Cache-Control: max-age=`.

Routing rules:

* The **first matching prefix** wins, in the order defined in the config.
* Once a route is selected, Warpgate:

  * picks an endpoint from the route's cluster (round-robin, health-aware),
  * rewrites the outgoing request's `URL.Scheme`, `URL.Host`, and `Host` header,
  * forwards the request and streams back the response.

---
