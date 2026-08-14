// SPDX-License-Identifier: CC0-1.0

package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adrg/xdg"
	"github.com/spf13/viper"
)

const (
	ServiceName = "splitkauf"
)

type Config struct {
	App      AppConfig      `mapstructure:"app"`
	Server   ServerConfig   `mapstructure:"server"`
	Metrics  MetricsConfig  `mapstructure:"metrics"`
	Database DatabaseConfig `mapstructure:"database"`
	Auth     AuthConfig     `mapstructure:"auth"`
}

type AppConfig struct {
	Name        string `mapstructure:"name"`
	Version     string `mapstructure:"version"`
	Environment string `mapstructure:"environment"`
	Debug       bool   `mapstructure:"debug"`
	LogLevel    string `mapstructure:"log_level"`
	// BaseURL is the externally reachable origin of the app (scheme + host),
	// used to build OIDC redirect/callback URLs. Empty in dev-auth mode.
	BaseURL string `mapstructure:"base_url"`
}

// AuthConfig groups authentication settings: the OIDC provider parameters and
// the server-side session settings. When no OIDC issuer/client is configured,
// the backend falls back to dev-auth.
type AuthConfig struct {
	OIDC     OIDCConfig     `mapstructure:"oidc"`
	Password PasswordConfig `mapstructure:"password"`
	Session  SessionConfig  `mapstructure:"session"`
}

// PasswordConfig holds the local username/password auth settings. Enabled is
// off by default; when set (and OIDC is not configured) the backend runs the
// operator-provisioned password flow instead of dev-auth.
type PasswordConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

// OIDCConfig holds the confidential-client OIDC parameters. All fields are
// empty in dev-auth mode; setting the issuer switches the backend to the OIDC
// BFF flow and makes client_id/client_secret/redirect_url required.
type OIDCConfig struct {
	Issuer                string `mapstructure:"issuer"`
	ClientID              string `mapstructure:"client_id"`
	ClientSecret          string `mapstructure:"client_secret"`
	RedirectURL           string `mapstructure:"redirect_url"`
	PostLogoutRedirectURL string `mapstructure:"post_logout_redirect_url"`
}

// SessionConfig controls the server-side session store cookie behaviour.
type SessionConfig struct {
	Lifetime     time.Duration `mapstructure:"lifetime"`
	CookieSecure bool          `mapstructure:"cookie_secure"`
}

// IsOIDCEnabled reports whether the OIDC BFF flow is configured. It requires the
// issuer, client id, and client secret to all be set; otherwise the backend runs
// in dev-auth mode.
func (c *Config) IsOIDCEnabled() bool {
	return c.Auth.OIDC.Issuer != "" &&
		c.Auth.OIDC.ClientID != "" &&
		c.Auth.OIDC.ClientSecret != ""
}

// IsPasswordEnabled reports whether local username/password auth is turned on
// (SPLITKAUF_AUTH_PASSWORD_ENABLED). It is independent of OIDC; the selection
// precedence (OIDC → password → dev-auth) is applied by the auth layer, so a
// deployment that sets both keeps OIDC.
func (c *Config) IsPasswordEnabled() bool {
	return c.Auth.Password.Enabled
}

// AuthMode names the active authentication mode for the public auth-config
// endpoint the frontend reads to choose its login UI.
type AuthMode string

const (
	AuthModeOIDC     AuthMode = "oidc"
	AuthModePassword AuthMode = "password"
	AuthModeDev      AuthMode = "dev"
)

// Mode returns the resolved authentication mode from config precedence:
// OIDC when configured, else password when enabled, else dev-auth.
func (c *Config) Mode() AuthMode {
	switch {
	case c.IsOIDCEnabled():
		return AuthModeOIDC
	case c.IsPasswordEnabled():
		return AuthModePassword
	default:
		return AuthModeDev
	}
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

// DatabaseConfig holds the PostgreSQL connection parameters. Pool-tuning fields
// (max open/idle conns, lifetimes) are intentionally omitted until needed.
type DatabaseConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	Name     string `mapstructure:"name"`
	SSLMode  string `mapstructure:"ssl_mode"`
}

// DSN builds the libpq-style keyword/value connection string for PostgreSQL.
// Every value is single-quoted with backslashes and single quotes escaped, so
// an empty value (e.g. an empty password) cannot swallow the following keyword
// and leave dbname unset.
func (d DatabaseConfig) DSN() string {
	return strings.Join([]string{
		"host=" + quoteDSNValue(d.Host),
		"port=" + quoteDSNValue(strconv.Itoa(d.Port)),
		"user=" + quoteDSNValue(d.User),
		"password=" + quoteDSNValue(d.Password),
		"dbname=" + quoteDSNValue(d.Name),
		"sslmode=" + quoteDSNValue(d.SSLMode),
	}, " ")
}

// quoteDSNValue wraps a libpq keyword/value string in single quotes, escaping
// embedded backslashes and single quotes as required by the libpq parser.
func quoteDSNValue(v string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `'`, `\'`)
	return "'" + replacer.Replace(v) + "'"
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
