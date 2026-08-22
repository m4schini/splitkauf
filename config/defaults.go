// SPDX-License-Identifier: CC0-1.0

package config

import (
	"time"

	"github.com/spf13/viper"
)

// Default values for the numeric settings, named so the numbers below are
// self-describing.
const (
	defaultServerPort      = 8080
	defaultMetricsPort     = 9090
	defaultDatabasePort    = 5432
	defaultSessionLifetime = 7 * 24 * time.Hour
)

func setDefaults(vpr *viper.Viper) {
	// App
	vpr.SetDefault("app.name", ServiceName)
	vpr.SetDefault("app.version", "0.0.0")
	vpr.SetDefault("app.environment", "development")
	vpr.SetDefault("app.debug", false)
	vpr.SetDefault("app.log_level", "info") // debug, info, warning, error
	vpr.SetDefault("app.base_url", "")

	// HTTP Server
	vpr.SetDefault("server.host", "0.0.0.0")
	vpr.SetDefault("server.port", defaultServerPort)

	// Metrics (Prometheus / OpenMetrics scrape endpoint)
	vpr.SetDefault("metrics.enabled", false)
	vpr.SetDefault("metrics.host", "0.0.0.0")
	vpr.SetDefault("metrics.port", defaultMetricsPort)
	vpr.SetDefault("metrics.path", "/metrics")

	// Database (PostgreSQL)
	vpr.SetDefault("database.host", "localhost")
	vpr.SetDefault("database.port", defaultDatabasePort)
	vpr.SetDefault("database.user", "splitkauf")
	vpr.SetDefault("database.password", "splitkauf")
	vpr.SetDefault("database.name", "splitkauf")
	vpr.SetDefault("database.ssl_mode", "disable")

	// Auth — OIDC (all empty → dev-auth fallback)
	vpr.SetDefault("auth.oidc.issuer", "")
	vpr.SetDefault("auth.oidc.client_id", "")
	vpr.SetDefault("auth.oidc.client_secret", "")
	vpr.SetDefault("auth.oidc.redirect_url", "")
	vpr.SetDefault("auth.oidc.post_logout_redirect_url", "")

	// Auth — password (local accounts; off unless explicitly enabled)
	vpr.SetDefault("auth.password.enabled", false)

	// Auth — session
	vpr.SetDefault("auth.session.lifetime", defaultSessionLifetime)
	vpr.SetDefault("auth.session.cookie_secure", true)
}
