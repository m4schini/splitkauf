// SPDX-License-Identifier: CC0-1.0

package config

import (
	"slices"
	"testing"
	"time"

	"github.com/spf13/viper"
)

// TestSetDefaults pins every baseline default registered by setDefaults. The
// values are the lowest-precedence layer of the config, so a silent change here
// changes the behaviour of every deployment that omits the key.
func TestSetDefaults(t *testing.T) {
	t.Parallel()

	vpr := viper.New()
	setDefaults(vpr)

	stringDefaults := []struct {
		key  string
		want string
	}{
		{key: "app.name", want: ServiceName},
		{key: "app.version", want: "0.0.0"},
		{key: "app.environment", want: "development"},
		{key: "app.log_level", want: "info"},
		{key: "app.base_url", want: ""},
		{key: "server.host", want: "0.0.0.0"},
		{key: "metrics.host", want: "0.0.0.0"},
		{key: "metrics.path", want: "/metrics"},
		{key: "database.host", want: "localhost"},
		{key: "database.user", want: ServiceName},
		{key: "database.password", want: ServiceName},
		{key: "database.name", want: ServiceName},
		{key: "database.ssl_mode", want: "disable"},
		{key: "auth.oidc.issuer", want: ""},
		{key: "auth.oidc.client_id", want: ""},
		{key: "auth.oidc.client_secret", want: ""},
		{key: "auth.oidc.redirect_url", want: ""},
		{key: "auth.oidc.post_logout_redirect_url", want: ""},
	}

	for _, tt := range stringDefaults {
		t.Run("string/"+tt.key, func(t *testing.T) {
			t.Parallel()

			if got := vpr.GetString(tt.key); got != tt.want {
				t.Errorf("GetString(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}

	intDefaults := []struct {
		key  string
		want int
	}{
		{key: "server.port", want: 8080},
		{key: "metrics.port", want: 9090},
		{key: "database.port", want: 5432},
	}

	for _, tt := range intDefaults {
		t.Run("int/"+tt.key, func(t *testing.T) {
			t.Parallel()

			if got := vpr.GetInt(tt.key); got != tt.want {
				t.Errorf("GetInt(%q) = %d, want %d", tt.key, got, tt.want)
			}
		})
	}

	boolDefaults := []struct {
		key  string
		want bool
	}{
		{key: "app.debug", want: false},
		{key: "metrics.enabled", want: false},
		{key: "auth.password.enabled", want: false},
		// Secure-by-default: session cookies must be marked Secure unless a
		// deployment explicitly opts out.
		{key: "auth.session.cookie_secure", want: true},
	}

	for _, tt := range boolDefaults {
		t.Run("bool/"+tt.key, func(t *testing.T) {
			t.Parallel()

			if got := vpr.GetBool(tt.key); got != tt.want {
				t.Errorf("GetBool(%q) = %t, want %t", tt.key, got, tt.want)
			}
		})
	}

	t.Run("duration/auth.session.lifetime", func(t *testing.T) {
		// Registered as a time.Duration (not an int or a string) so that
		// Unmarshal populates the config struct without a decode hook.
		want := 7 * 24 * time.Hour
		if got := vpr.GetDuration("auth.session.lifetime"); got != want {
			t.Errorf("GetDuration(auth.session.lifetime) = %v, want %v", got, want)
		}

		if _, ok := vpr.Get("auth.session.lifetime").(time.Duration); !ok {
			t.Errorf("auth.session.lifetime = %T, want time.Duration", vpr.Get("auth.session.lifetime"))
		}
	})
}

// TestSetDefaultsRegistersEmptyKeys ensures the empty-string OIDC defaults are
// registered rather than absent: the dev-auth fallback downstream keys off the
// values being empty, and a missing key would also break Unmarshal expectations.
func TestSetDefaultsRegistersEmptyKeys(t *testing.T) {
	t.Parallel()

	vpr := viper.New()
	setDefaults(vpr)

	keys := []string{
		"app.base_url",
		"auth.oidc.issuer",
		"auth.oidc.client_id",
		"auth.oidc.client_secret",
		"auth.oidc.redirect_url",
		"auth.oidc.post_logout_redirect_url",
	}

	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			if !vpr.IsSet(key) {
				t.Errorf("IsSet(%q) = false, want true", key)
			}

			if got := vpr.GetString(key); got != "" {
				t.Errorf("GetString(%q) = %q, want empty string", key, got)
			}
		})
	}
}

// TestSetDefaultsAreLowestPrecedence verifies setDefaults uses SetDefault and
// not Set, so config file, env and explicit values still win.
func TestSetDefaultsAreLowestPrecedence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  string
		set  any
		want string
	}{
		{name: "port override", key: "server.port", set: 9999, want: "9999"},
		{name: "environment override", key: "app.environment", set: "production", want: "production"},
		{name: "cookie_secure override", key: "auth.session.cookie_secure", set: false, want: "false"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vpr := viper.New()
			setDefaults(vpr)
			vpr.Set(tt.key, tt.set)

			if got := vpr.GetString(tt.key); got != tt.want {
				t.Errorf("GetString(%q) after Set = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

// TestSetDefaultsIdempotent pins that a second call is harmless, in case the
// sync.Once guarding Load() ever goes away.
func TestSetDefaultsIdempotent(t *testing.T) {
	t.Parallel()

	vpr := viper.New()
	setDefaults(vpr)
	setDefaults(vpr)

	if got := vpr.GetInt("server.port"); got != 8080 {
		t.Errorf("GetInt(server.port) = %d, want 8080", got)
	}

	if got := vpr.GetString("app.name"); got != ServiceName {
		t.Errorf("GetString(app.name) = %q, want %q", got, ServiceName)
	}

	if got := vpr.GetDuration("auth.session.lifetime"); got != 7*24*time.Hour {
		t.Errorf("GetDuration(auth.session.lifetime) = %v, want %v", got, 7*24*time.Hour)
	}
}

// TestSetDefaultsNilViperPanics documents the absence of a nil guard. The sole
// caller passes viper.New(), so this is unreachable in production.
func TestSetDefaultsNilViperPanics(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("setDefaults(nil) did not panic, want nil-pointer dereference")
		}
	}()

	setDefaults(nil)
}

// TestSetDefaultsRegistersExactKeySet pins the complete key set registered by
// setDefaults. The per-key assertions in TestSetDefaults can only fail for keys
// they already name, so a default that is added, renamed or dropped would
// otherwise slip through the suite unnoticed.
func TestSetDefaultsRegistersExactKeySet(t *testing.T) {
	t.Parallel()

	// Sorted, because viper.AllKeys has no defined order.
	want := []string{
		"app.base_url",
		"app.debug",
		"app.environment",
		"app.log_level",
		"app.name",
		"app.version",
		"auth.oidc.client_id",
		"auth.oidc.client_secret",
		"auth.oidc.issuer",
		"auth.oidc.post_logout_redirect_url",
		"auth.oidc.redirect_url",
		"auth.password.enabled",
		"auth.session.cookie_secure",
		"auth.session.lifetime",
		"database.host",
		"database.name",
		"database.password",
		"database.port",
		"database.ssl_mode",
		"database.user",
		"metrics.enabled",
		"metrics.host",
		"metrics.path",
		"metrics.port",
		"server.host",
		"server.port",
	}

	vpr := viper.New()
	setDefaults(vpr)

	// Nothing but setDefaults has touched vpr, so AllKeys is exactly the set of
	// registered defaults.
	got := vpr.AllKeys()
	slices.Sort(got)

	if slices.Equal(got, want) {
		return
	}

	for _, key := range want {
		if !slices.Contains(got, key) {
			t.Errorf("default %q is not registered, want it registered", key)
		}
	}

	for _, key := range got {
		if !slices.Contains(want, key) {
			t.Errorf("unexpected default %q; add it to this test and to TestSetDefaults", key)
		}
	}

	// Guards against a duplicate or case-folded key slipping past the two
	// membership loops above.
	if len(got) != len(want) {
		t.Errorf("AllKeys() has %d keys, want %d: got %q", len(got), len(want), got)
	}
}
