// SPDX-License-Identifier: CC0-1.0

// Package metrics defines the Prometheus/OpenMetrics collectors emitted by
// the service and exposes them on a dedicated HTTP listener. Metric names follow
// the OpenMetrics conventions (snake_case, base-unit suffixes such as
// _seconds / _bytes / _total).
package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/m4schini/splitkauf/config"
)

const (
	namespace     = config.ServiceName
	subsystemHTTP = "http"

	labelMethod = "method"
	labelRoute  = "route"
	labelStatus = "status"

	// Response-size histogram buckets: 64B → ~1MB.
	responseSizeStartBytes  = 64
	responseSizeFactor      = 4
	responseSizeBucketCount = 8
)

// The collectors below are package-level singletons: the whole point of the
// package is one process-wide metric registry shared by every instrumented
// call site.
//
//nolint:gochecknoglobals // package-level metric registry and collectors, singleton by design
var (
	// Registry is the application metric registry. It is intentionally a
	// fresh registry rather than prometheus.DefaultRegisterer so we control
	// which collectors are exposed.
	Registry = prometheus.NewRegistry()

	// registerOnce guards the one-time collector registration performed by
	// ensureRegistered.
	registerOnce sync.Once

	// ── HTTP (RED) ──────────────────────────────────────────────────────.

	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace:   namespace,
			Subsystem:   subsystemHTTP,
			Name:        "requests_total",
			Help:        "Total number of HTTP requests served, partitioned by route, method and status.",
			ConstLabels: nil,
		},
		[]string{labelMethod, labelRoute, labelStatus},
	)

	httpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace:                       namespace,
			Subsystem:                       subsystemHTTP,
			Name:                            "request_duration_seconds",
			Help:                            "HTTP request latency in seconds.",
			ConstLabels:                     nil,
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     0,
			NativeHistogramZeroThreshold:    0,
			NativeHistogramMaxBucketNumber:  0,
			NativeHistogramMinResetDuration: 0,
			NativeHistogramMaxZeroThreshold: 0,
			NativeHistogramMaxExemplars:     0,
			NativeHistogramExemplarTTL:      0,
		},
		[]string{labelMethod, labelRoute, labelStatus},
	)

	httpResponseSize = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace:   namespace,
			Subsystem:   subsystemHTTP,
			Name:        "response_size_bytes",
			Help:        "HTTP response body size in bytes.",
			ConstLabels: nil,
			Buckets: prometheus.ExponentialBuckets(
				responseSizeStartBytes, responseSizeFactor, responseSizeBucketCount,
			),
			NativeHistogramBucketFactor:     0,
			NativeHistogramZeroThreshold:    0,
			NativeHistogramMaxBucketNumber:  0,
			NativeHistogramMinResetDuration: 0,
			NativeHistogramMaxZeroThreshold: 0,
			NativeHistogramMaxExemplars:     0,
			NativeHistogramExemplarTTL:      0,
		},
		[]string{labelMethod, labelRoute},
	)

	httpInFlight = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace:   namespace,
			Subsystem:   subsystemHTTP,
			Name:        "requests_in_flight",
			Help:        "Number of HTTP requests currently being served.",
			ConstLabels: nil,
		},
	)

	// ── Build info ──────────────────────────────────────────────────────.

	buildInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace:   namespace,
			Subsystem:   "",
			Name:        "build_info",
			Help:        "Constant 1 gauge labelled with build metadata.",
			ConstLabels: nil,
		},
		[]string{"version", "environment"},
	)
)

// ensureRegistered registers every collector with Registry exactly once. The
// public entry points call it lazily, replacing the former init function, so
// call sites need no explicit registration step.
func ensureRegistered() {
	registerOnce.Do(func() {
		Registry.MustRegister(
			httpRequestsTotal,
			httpRequestDuration,
			httpResponseSize,
			httpInFlight,
			buildInfo,
			collectors.NewGoCollector(),
			collectors.NewProcessCollector(collectors.ProcessCollectorOpts{
				PidFn:        nil,
				Namespace:    "",
				ReportErrors: false,
			}),
		)
	})
}

// SetBuildInfo seeds the build_info gauge. Call once during startup.
func SetBuildInfo(version, environment string) {
	ensureRegistered()
	buildInfo.WithLabelValues(version, environment).Set(1)
}

// ── Public helpers used by instrumented call sites ──────────────────────
