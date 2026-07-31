// SPDX-License-Identifier: TODO

package config

import "github.com/spf13/viper"

func setDefaults(v *viper.Viper) {
	// App
	v.SetDefault("app.name", ServiceName)
	v.SetDefault("app.version", "0.0.0")
	v.SetDefault("app.environment", "development")
	v.SetDefault("app.debug", false)
	v.SetDefault("app.log_level", "info") // debug, info, warning, error

	// HTTP Server
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 8080)

	// Metrics (Prometheus / OpenMetrics scrape endpoint)
	v.SetDefault("metrics.enabled", false)
	v.SetDefault("metrics.host", "0.0.0.0")
	v.SetDefault("metrics.port", 9090)
	v.SetDefault("metrics.path", "/metrics")
}
