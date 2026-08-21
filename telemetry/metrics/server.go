// SPDX-License-Identifier: CC0-1.0

package metrics

import (
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Handler returns the HTTP handler that serves the metrics endpoint. It
// emits OpenMetrics when the scraper negotiates
// "application/openmetrics-text" via Accept; otherwise it falls back to the
// Prometheus text exposition format.
func Handler() http.Handler {
	return promhttp.HandlerFor(Registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
		Registry:          Registry,
	})
}

// NewServer wires the metrics handler at the configured path on a fresh
// http.Server bound to host:port. The caller owns the lifecycle (Start /
// Shutdown).
func NewServer(host string, port int, path string) *http.Server {
	mux := http.NewServeMux()
	mux.Handle(path, Handler())

	return &http.Server{
		Addr:              net.JoinHostPort(host, strconv.Itoa(port)),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
}
