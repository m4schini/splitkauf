// SPDX-License-Identifier: CC0-1.0

package config

import "testing"

const (
	testIssuer      = "https://idp.example.com"
	testShortIssuer = "https://idp"
	testSecret      = "secret"
)

// validBase returns a minimal Config that passes validation, so tests can
// override only the auth fields they care about.
func validBase() *Config {
	return &Config{
		App: AppConfig{
			Name:        ServiceName,
			Version:     "",
			Environment: "",
			Debug:       false,
			LogLevel:    "info",
			BaseURL:     "",
		},
		Server: ServerConfig{Host: "0.0.0.0", Port: 8080},
		Metrics: MetricsConfig{
			Enabled: false,
			Host:    "",
			Port:    0,
			Path:    "",
		},
		Database: DatabaseConfig{
			Host:     "",
			Port:     5432,
			User:     ServiceName,
			Password: "",
			Name:     ServiceName,
			SSLMode:  "",
		},
		Auth: AuthConfig{
			OIDC: OIDCConfig{
				Issuer:                "",
				ClientID:              "",
				ClientSecret:          "",
				RedirectURL:           "",
				PostLogoutRedirectURL: "",
			},
			Password: PasswordConfig{Enabled: false},
			Session:  SessionConfig{Lifetime: 0, CookieSecure: false},
		},
	}
}

func TestValidateAuthOIDC(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		oidc    OIDCConfig
		wantErr bool
	}{
		{
			name: "oidc complete is valid",
			oidc: OIDCConfig{
				Issuer:                testIssuer,
				ClientID:              "client",
				ClientSecret:          testSecret,
				RedirectURL:           "https://app.example.com/api/auth/callback",
				PostLogoutRedirectURL: "",
			},
			wantErr: false,
		},
		{
			name: "issuer only is invalid",
			oidc: OIDCConfig{
				Issuer:                testIssuer,
				ClientID:              "",
				ClientSecret:          "",
				RedirectURL:           "",
				PostLogoutRedirectURL: "",
			},
			wantErr: true,
		},
		{
			name: "issuer without redirect_url is invalid",
			oidc: OIDCConfig{
				Issuer:                testIssuer,
				ClientID:              "client",
				ClientSecret:          testSecret,
				RedirectURL:           "",
				PostLogoutRedirectURL: "",
			},
			wantErr: true,
		},
		{
			name: "all empty is valid (dev mode)",
			oidc: OIDCConfig{
				Issuer:                "",
				ClientID:              "",
				ClientSecret:          "",
				RedirectURL:           "",
				PostLogoutRedirectURL: "",
			},
			wantErr: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cfg := validBase()
			cfg.Auth.OIDC = testCase.oidc

			err := validate(cfg)
			if (err != nil) != testCase.wantErr {
				t.Fatalf("validate() error = %v, wantErr %v", err, testCase.wantErr)
			}
		})
	}
}

func TestIsOIDCEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		oidc OIDCConfig
		want bool
	}{
		{
			name: "all set is enabled",
			oidc: OIDCConfig{
				Issuer:                testShortIssuer,
				ClientID:              "c",
				ClientSecret:          "s",
				RedirectURL:           "",
				PostLogoutRedirectURL: "",
			},
			want: true,
		},
		{
			name: "all empty is disabled",
			oidc: OIDCConfig{
				Issuer:                "",
				ClientID:              "",
				ClientSecret:          "",
				RedirectURL:           "",
				PostLogoutRedirectURL: "",
			},
			want: false,
		},
		{
			name: "issuer only is disabled",
			oidc: OIDCConfig{
				Issuer:                testShortIssuer,
				ClientID:              "",
				ClientSecret:          "",
				RedirectURL:           "",
				PostLogoutRedirectURL: "",
			},
			want: false,
		},
		{
			name: "missing client secret is disabled",
			oidc: OIDCConfig{
				Issuer:                testShortIssuer,
				ClientID:              "c",
				ClientSecret:          "",
				RedirectURL:           "",
				PostLogoutRedirectURL: "",
			},
			want: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cfg := validBase()

			cfg.Auth.OIDC = testCase.oidc
			if got := cfg.IsOIDCEnabled(); got != testCase.want {
				t.Errorf("IsOIDCEnabled() = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestModePrecedence(t *testing.T) {
	t.Parallel()

	oidc := OIDCConfig{
		Issuer:                testShortIssuer,
		ClientID:              "c",
		ClientSecret:          "s",
		RedirectURL:           "",
		PostLogoutRedirectURL: "",
	}
	emptyOIDC := OIDCConfig{
		Issuer:                "",
		ClientID:              "",
		ClientSecret:          "",
		RedirectURL:           "",
		PostLogoutRedirectURL: "",
	}

	tests := []struct {
		name     string
		oidc     OIDCConfig
		password bool
		want     AuthMode
	}{
		{"both configured is combined", oidc, true, AuthModeCombined},
		{"oidc only", oidc, false, AuthModeOIDC},
		{"password when oidc unset", emptyOIDC, true, AuthModePassword},
		{"dev when neither", emptyOIDC, false, AuthModeDev},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cfg := validBase()
			cfg.Auth.OIDC = testCase.oidc

			cfg.Auth.Password.Enabled = testCase.password
			if got := cfg.Mode(); got != testCase.want {
				t.Errorf("Mode() = %q, want %q", got, testCase.want)
			}

			if got := cfg.IsPasswordEnabled(); got != testCase.password {
				t.Errorf("IsPasswordEnabled() = %v, want %v", got, testCase.password)
			}
		})
	}
}
