// SPDX-License-Identifier: TODO

package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/adrg/xdg"
	"github.com/spf13/viper"
)

const (
	ServiceName = "splitkauf"
)

type Config struct {
	App     AppConfig     `mapstructure:"app"`
	Server  ServerConfig  `mapstructure:"server"`
	Metrics MetricsConfig `mapstructure:"metrics"`
}

type AppConfig struct {
	Name        string `mapstructure:"name"`
	Version     string `mapstructure:"version"`
	Environment string `mapstructure:"environment"`
	Debug       bool   `mapstructure:"debug"`
	LogLevel    string `mapstructure:"log_level"`
}

type ServerConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

// MetricsConfig controls the dedicated Prometheus/OpenMetrics scrape endpoint.
// It listens on its own port, separate from the public API, so it can be
// excluded from Ingress and scraped only from inside the cluster.
type MetricsConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Host    string `mapstructure:"host"`
	Port    int    `mapstructure:"port"`
	Path    string `mapstructure:"path"`
}

var (
	C       *Config
	cfgOnce sync.Once
)

// Load reads configuration from file + environment, validates it,
// and returns the parsed Config. Safe for concurrent use.
func Load() error {
	var loadErr error

	cfgOnce.Do(func() {
		v := viper.New()

		// ── 1. Set defaults ──────────────────────────────
		setDefaults(v)

		// ── 2. Config file ───────────────────────────────
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath("./config")
		v.AddConfigPath("/etc/" + strings.ToLower(ServiceName))
		v.AddConfigPath(filepath.Join(xdg.ConfigHome, strings.ToLower(ServiceName)))

		if err := v.ReadInConfig(); err != nil {
			if errors.Is(err, viper.ConfigFileNotFoundError{}) {
				loadErr = fmt.Errorf("reading config file: %w", err)
				return
			}
			// Config file not found — rely on defaults + env
		}

		// Environment variables
		v.SetEnvPrefix(strings.ToUpper(ServiceName))
		v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
		v.AutomaticEnv()

		// Unmarshal into struct
		parsedCfg := &Config{}
		if err := v.Unmarshal(parsedCfg); err != nil {
			loadErr = fmt.Errorf("unmarshaling config: %w", err)
			return
		}

		// Validate
		if err := validate(parsedCfg); err != nil {
			loadErr = fmt.Errorf("config validation: %w", err)
			return
		}

		C = parsedCfg
	})

	return loadErr
}
