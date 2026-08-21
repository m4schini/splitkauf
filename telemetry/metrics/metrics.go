// SPDX-License-Identifier: CC0-1.0

// Package metrics defines the Prometheus/OpenMetrics collectors emitted by
// the service and exposes them on a dedicated HTTP listener. Metric names follow
// the OpenMetrics conventions (snake_case, base-unit suffixes such as
// _seconds / _bytes / _total).
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/m4schini/splitkauf/config"
)

const namespace = config.ServiceName

var (
	// Registry is the application metric registry. It is intentionally a
	// fresh registry rather than prometheus.DefaultRegisterer so we control
	// which collectors are exposed.
	Registry = prometheus.NewRegistry()

	// ── HTTP (RED) ──────────────────────────────────────────────────────

	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "Total number of HTTP requests served, partitioned by route, method and status.",
		},
		[]string{"method", "route", "status"},
	)

	httpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "HTTP request latency in seconds.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"method", "route", "status"},
	)

	httpResponseSize = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "response_size_bytes",
			Help:      "HTTP response body size in bytes.",
			Buckets:   prometheus.ExponentialBuckets(64, 4, 8), // 64B → ~1MB
		},
		[]string{"method", "route"},
	)

	httpInFlight = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "requests_in_flight",
			Help:      "Number of HTTP requests currently being served.",
		},
	)

	// ── Build info ──────────────────────────────────────────────────────

	buildInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "build_info",
			Help:      "Constant 1 gauge labelled with build metadata.",
		},
		[]string{"version", "environment"},
	)
)

func init() {
	Registry.MustRegister(
		httpRequestsTotal,
		httpRequestDuration,
		httpResponseSize,
		httpInFlight,
		buildInfo,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
}

// SetBuildInfo seeds the build_info gauge. Call once during startup.
func SetBuildInfo(version, environment string) {
	buildInfo.WithLabelValues(version, environment).Set(1)
}

// ── Public helpers used by instrumented call sites ──────────────────────
