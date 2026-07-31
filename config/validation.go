// SPDX-License-Identifier: TODO

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
