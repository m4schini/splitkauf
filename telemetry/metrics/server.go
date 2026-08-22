// SPDX-License-Identifier: CC0-1.0

package metrics

import (
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// readHeaderTimeout bounds how long the metrics server waits for request
// headers, protecting the listener against slowloris-style clients.
const readHeaderTimeout = 5 * time.Second

// Handler returns the HTTP handler that serves the metrics endpoint. It
// emits OpenMetrics when the scraper negotiates
// "application/openmetrics-text" via Accept; otherwise it falls back to the
// Prometheus text exposition format.
func Handler() http.Handler {
	ensureRegistered()

	return promhttp.HandlerFor(Registry, promhttp.HandlerOpts{
		ErrorLog:                            nil,
		ErrorHandling:                       promhttp.HTTPErrorOnError,
		Registry:                            Registry,
		DisableCompression:                  false,
		OfferedCompressions:                 nil,
		MaxRequestsInFlight:                 0,
		Timeout:                             0,
		EnableOpenMetrics:                   true,
		EnableOpenMetricsTextCreatedSamples: false,
		ProcessStartTime:                    time.Time{},
	})
}

// NewServer wires the metrics handler at the configured path on a fresh
// http.Server bound to host:port. The caller owns the lifecycle (Start /
// Shutdown).
func NewServer(host string, port int, path string) *http.Server {
	mux := http.NewServeMux()
	mux.Handle(path, Handler())

	return &http.Server{
		Addr:                         net.JoinHostPort(host, strconv.Itoa(port)),
		Handler:                      mux,
		DisableGeneralOptionsHandler: false,
		TLSConfig:                    nil,
		ReadTimeout:                  0,
		ReadHeaderTimeout:            readHeaderTimeout,
		WriteTimeout:                 0,
		IdleTimeout:                  0,
		MaxHeaderBytes:               0,
		TLSNextProto:                 nil,
		ConnState:                    nil,
		ErrorLog:                     nil,
		BaseContext:                  nil,
		ConnContext:                  nil,
		HTTP2:                        nil,
		Protocols:                    nil,
	}
}
