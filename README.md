# WarpGate

Warpgate is a small HTTP reverse proxy written in Go

- Route -> Cluster -> Endpoint model
- Round-robin load balancing
- Active health checks + basic circuit breaker
- Respinse caching
- Structured logging and metrics
- Middleware chaining (IP filter)
- Multi-listener support with HTTP -> HTTPS redirect

## Features

- **Routing**
  - Path-Prefix based routing
  - Each route maps to a named *cluster* instead of a single upstream URL

- **Clusters & Load Balancing**
  - Clusters group multiple upstream endpoints
  - Round-robin load balancing
  - Active health checks (`/health` or configurable path)
  - Simple circuit breaker: trip after N failures, cooldown window

- **Caching**
  - Per-route, TTL-based c ache
  - Hnors `cache-control: max-age=` where present
  - only caches `GET`/`HEAD`, skips `private` or `no-store`

- **Listeners**
  - Multiple listners from config
  - HTTP -> HTTPS redirect listeners

- **Midleware**
  - Middlerware chain around the proxy engine
  - IP filer middleware (CIDR blocklist)

## quick start (local)

### 1. clone & build

```bash
git clone https://github,com/tunedev/warpgate.git
cd warpgate

go test ./...
go build -o bin/warpgate ./cmd/warpgate
```

### 2. Run a demo upstream

For a quick demo, run a simple HTTP server on `:9000`:

```bash
go run ./hack/demo-backend/main.go
```

*(Or use any existing service that responds on `http://localhost:9000`.)*

### 3. Configure Warpgate

Minimal example config:

```yaml
# configs/warpgate.yaml

server:
  address: ":8080"
  tls:
    enabled: false
  ipBlockCIDRs: []  # optional

cache:
  maxEntries: 1000
  defaultTTL: 30s
  maxBodyBytes: 1048576

clusters:
  - name: api_cluster
    endpoints:
      - "http://localhost:9000"
    healthCheck:
      path: "/health"
      interval: 5s
      timeout: 1s
      unhealthyThreshold: 3
      healthyThreshold: 1
    circuitBreaker:
      consecutiveFailures: 5
      cooldown: 30s

routes:
  - name: api
    pathPrefix: "/api"
    cluster: "api_cluster"
    cache:
      enabled: false

# Optional listeners; if omitted, falls back to server.address
listeners:
  - name: http
    address: ":8080"
    tls:
      enabled: false
    redirectTo: "https"

  - name: https
    address: ":8443"
    tls:
      enabled: true
      certFile: "cert.pem"
      keyFile: "key.pem"
```

### 4. Run Warpgate

```bash
go run ./cmd/warpgate/main.go --config configs/warpgate.yaml
```

Then call it:

```bash
curl -i http://localhost:8080/api/hello
```

If you have HTTP→HTTPS redirect configured:

```bash
curl -i http://localhost:8080/api/hello      # 301 -> https://...
curl -k https://localhost:8443/api/hello    # actual upstream response
```

Metrics:

```bash
curl http://localhost:8080/metrics
```

---

## Running with Docker

### Build image

```bash
docker build -t warpgate:local .
```

### Run with a mounted config

```bash
docker run --rm \
  -p 8080:8080 \
  -p 8443:8443 \
  -v $(pwd)/configs/warpgate.yaml:/app/configs/warpgate.yaml:ro \
  -v $(pwd)/cert.pem:/app/cert.pem:ro \
  -v $(pwd)/key.pem:/app/key.pem:ro \
  warpgate:local \
  --config /app/configs/warpgate.yaml
```

You can then hit `http://localhost:8080/...` or `https://localhost:8443/...` from your host.

---

## Example docker-compose demo

See `docker-compose.yaml` for a demo with:

- `demo-backend` on `:9000`
- `warpgate` in front of it, exposing `:8080`

Run:

```bash
docker-compose up --build
```

Then:

```bash
curl http://localhost:8080/api/hello
```

---

## Configuration

See [CONFIGURATION.md](./CONFIGURATION.md) for a full breakdown of the YAML configuration:

- `server`
- `listeners`
- `cache`
- `clusters`
- `routes`

---
