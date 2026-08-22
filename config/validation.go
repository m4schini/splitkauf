// SPDX-License-Identifier: CC0-1.0

package config

import (
	"errors"
	"fmt"
	"slices"
)

// Port bounds for the TCP listeners and the database connection.
const (
	minPort = 1
	maxPort = 65535
)

// Static validation errors. Dynamic context (the offending value) is attached
// at the call site via fmt.Errorf with %w, so callers can still match with
// errors.Is.
var (
	errAppNameRequired          = errors.New("app.name is required")
	errServerPortRange          = errors.New("server.http.port must be 1–65535")
	errMetricsPortRange         = errors.New("metrics.port must be 1–65535")
	errMetricsPortConflict      = errors.New("metrics.port must differ from server.port")
	errMetricsPathFormat        = errors.New("metrics.path must start with '/'")
	errDatabasePortRange        = errors.New("database.port must be 1–65535")
	errDatabaseNameRequired     = errors.New("database.name is required")
	errDatabaseUserRequired     = errors.New("database.user is required")
	errOIDCClientIDRequired     = errors.New("auth.oidc.client_id is required when auth.oidc.issuer is set")
	errOIDCClientSecretRequired = errors.New("auth.oidc.client_secret is required when auth.oidc.issuer is set")
	errOIDCRedirectURLRequired  = errors.New("auth.oidc.redirect_url is required when auth.oidc.issuer is set")
	errLogLevelInvalid          = errors.New("app.log_level must be one of debug/info/warn/error")
)

func validate(cfg *Config) error {
	errs := slices.Concat(
		validateApp(&cfg.App),
		validateServer(&cfg.Server),
		validateMetrics(&cfg.Metrics, cfg.Server.Port),
		validateDatabase(&cfg.Database),
		validateAuth(&cfg.Auth),
	)

	if len(errs) > 0 {
		return fmt.Errorf("config validation failed:\n  %w", errors.Join(errs...))
	}

	return nil
}

func validateApp(app *AppConfig) []error {
	var errs []error

	if app.Name == "" {
		errs = append(errs, errAppNameRequired)
	}

	validLogLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLogLevels[app.LogLevel] {
		errs = append(errs, fmt.Errorf("%w, got %q", errLogLevelInvalid, app.LogLevel))
	}

	return errs
}

func validateServer(server *ServerConfig) []error {
	var errs []error

	if server.Port < minPort || server.Port > maxPort {
		errs = append(errs, fmt.Errorf("%w, got %d", errServerPortRange, server.Port))
	}

	return errs
}

func validateMetrics(metrics *MetricsConfig, serverPort int) []error {
	if !metrics.Enabled {
		return nil
	}

	var errs []error

	if metrics.Port < minPort || metrics.Port > maxPort {
		errs = append(errs, fmt.Errorf("%w, got %d", errMetricsPortRange, metrics.Port))
	}

	if metrics.Port == serverPort {
		errs = append(errs, fmt.Errorf("%w, got %d for both", errMetricsPortConflict, metrics.Port))
	}

	if metrics.Path == "" || metrics.Path[0] != '/' {
		errs = append(errs, fmt.Errorf("%w, got %q", errMetricsPathFormat, metrics.Path))
	}

	return errs
}

func validateDatabase(database *DatabaseConfig) []error {
	var errs []error

	if database.Port < minPort || database.Port > maxPort {
		errs = append(errs, fmt.Errorf("%w, got %d", errDatabasePortRange, database.Port))
	}

	if database.Name == "" {
		errs = append(errs, errDatabaseNameRequired)
	}

	if database.User == "" {
		errs = append(errs, errDatabaseUserRequired)
	}

	return errs
}

// validateAuth checks the OIDC settings. OIDC is optional: an empty issuer
// means dev-auth. When an issuer is set, the confidential-client parameters
// become required.
func validateAuth(auth *AuthConfig) []error {
	if auth.OIDC.Issuer == "" {
		return nil
	}

	var errs []error

	if auth.OIDC.ClientID == "" {
		errs = append(errs, errOIDCClientIDRequired)
	}

	if auth.OIDC.ClientSecret == "" {
		errs = append(errs, errOIDCClientSecretRequired)
	}

	if auth.OIDC.RedirectURL == "" {
		errs = append(errs, errOIDCRedirectURLRequired)
	}

	return errs
}
