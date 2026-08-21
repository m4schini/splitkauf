// SPDX-License-Identifier: CC0-1.0

package config

import (
	"errors"
	"fmt"
)

func validate(c *Config) error {
	var errs []error

	// ── Required fields ──────────────────────────────────
	if c.App.Name == "" {
		errs = append(errs, errors.New("app.name is required"))
	}

	// ── Server ports ─────────────────────────────────────
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		errs = append(errs, fmt.Errorf("server.http.port must be 1–65535, got %d", c.Server.Port))
	}

	if c.Metrics.Enabled {
		if c.Metrics.Port < 1 || c.Metrics.Port > 65535 {
			errs = append(errs, fmt.Errorf("metrics.port must be 1–65535, got %d", c.Metrics.Port))
		}

		if c.Metrics.Port == c.Server.Port {
			errs = append(errs, fmt.Errorf("metrics.port (%d) must differ from server.port", c.Metrics.Port))
		}

		if c.Metrics.Path == "" || c.Metrics.Path[0] != '/' {
			errs = append(errs, fmt.Errorf("metrics.path must start with '/', got %q", c.Metrics.Path))
		}
	}

	// ── Database ─────────────────────────────────────────
	if c.Database.Port < 1 || c.Database.Port > 65535 {
		errs = append(errs, fmt.Errorf("database.port must be 1–65535, got %d", c.Database.Port))
	}

	if c.Database.Name == "" {
		errs = append(errs, errors.New("database.name is required"))
	}

	if c.Database.User == "" {
		errs = append(errs, errors.New("database.user is required"))
	}

	// ── Auth (OIDC) ──────────────────────────────────────
	// OIDC is optional: an empty issuer means dev-auth. When an issuer is set,
	// the confidential-client parameters become required.
	if c.Auth.OIDC.Issuer != "" {
		if c.Auth.OIDC.ClientID == "" {
			errs = append(errs, errors.New("auth.oidc.client_id is required when auth.oidc.issuer is set"))
		}

		if c.Auth.OIDC.ClientSecret == "" {
			errs = append(errs, errors.New("auth.oidc.client_secret is required when auth.oidc.issuer is set"))
		}

		if c.Auth.OIDC.RedirectURL == "" {
			errs = append(errs, errors.New("auth.oidc.redirect_url is required when auth.oidc.issuer is set"))
		}
	}

	// ── Allowed values ───────────────────────────────────
	validLogLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLogLevels[c.App.LogLevel] {
		errs = append(errs, fmt.Errorf("app.log_level must be one of debug/info/warn/error/fatal, got %q", c.App.LogLevel))
	}

	if len(errs) > 0 {
		return fmt.Errorf("config validation failed:\n  %w", errors.Join(errs...))
	}

	return nil
}
