package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	requestTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "warpgate",
			Name:      "http_requests_total",
			Help:      "Total number of HTTP requests handled by warpgate",
		},
		[]string{"route", "method", "code"},
	)

	requestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "warpgate",
			Name:      "http_request_duration_seconds",
			Help:      "Duration of HTTP requests handled by warpgate",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"route", "method"},
	)

	cacheHits = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "warpgate",
			Name:      "cache_hits_total",
			Help:      "Total cache hits",
		},
		[]string{"route"},
	)

	cacheMisses = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "warpgate",
			Name:      "cache_misses_total",
			Help:      "Total cache misses",
		},
		[]string{"route"},
	)

	clusterUnhealthy = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "warpgate",
			Name:      "cluster_unhealthy_endpoints",
			Help:      "Number of unhealthy endpoints per cluster",
		},
		[]string{"cluster"},
	)
)

func Init() {
	prometheus.MustRegister(requestTotal, requestDuration, cacheHits, cacheMisses, clusterUnhealthy)
}

func Handler() http.Handler {
	return promhttp.Handler()
}

func ObserveRequest(route, method, code string, d time.Duration) {
	requestTotal.WithLabelValues(route, method, code).Inc()
	requestDuration.WithLabelValues(route, method).Observe(d.Seconds())
}

func IncCacheHit(route string) {
	cacheHits.WithLabelValues(route).Inc()
}

func IncCacheMiss(route string) {
	cacheMisses.WithLabelValues(route).Inc()
}

func SetClusterUnhealthy(cluster string, value float64) {
	clusterUnhealthy.WithLabelValues(cluster).Set(value)
}
