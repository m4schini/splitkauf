// SPDX-License-Identifier: TODO

package config

import (
	"time"

	"github.com/spf13/viper"
)

func setDefaults(v *viper.Viper) {
	// App
	v.SetDefault("app.name", ServiceName)
	v.SetDefault("app.version", "0.0.0")
	v.SetDefault("app.environment", "development")
	v.SetDefault("app.debug", false)
	v.SetDefault("app.log_level", "info") // debug, info, warning, error
	v.SetDefault("app.base_url", "")

	// HTTP Server
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 8080)

	// Metrics (Prometheus / OpenMetrics scrape endpoint)
	v.SetDefault("metrics.enabled", false)
	v.SetDefault("metrics.host", "0.0.0.0")
	v.SetDefault("metrics.port", 9090)
	v.SetDefault("metrics.path", "/metrics")

	// Database (PostgreSQL)
	v.SetDefault("database.host", "localhost")
	v.SetDefault("database.port", 5432)
	v.SetDefault("database.user", "splitkauf")
	v.SetDefault("database.password", "splitkauf")
	v.SetDefault("database.name", "splitkauf")
	v.SetDefault("database.ssl_mode", "disable")

	// Auth — OIDC (all empty → dev-auth fallback)
	v.SetDefault("auth.oidc.issuer", "")
	v.SetDefault("auth.oidc.client_id", "")
	v.SetDefault("auth.oidc.client_secret", "")
	v.SetDefault("auth.oidc.redirect_url", "")
	v.SetDefault("auth.oidc.post_logout_redirect_url", "")

	// Auth — session
	v.SetDefault("auth.session.lifetime", 168*time.Hour)
	v.SetDefault("auth.session.cookie_secure", true)
}
