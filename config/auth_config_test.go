// SPDX-License-Identifier: TODO

package config

import "testing"

// validBase returns a minimal Config that passes validation, so tests can
// override only the auth fields they care about.
func validBase() *Config {
	return &Config{
		App: AppConfig{
			Name:     ServiceName,
			LogLevel: "info",
		},
		Server:   ServerConfig{Host: "0.0.0.0", Port: 8080},
		Database: DatabaseConfig{Port: 5432, User: "splitkauf", Name: "splitkauf"},
	}
}

func TestValidateAuthOIDC(t *testing.T) {
	tests := []struct {
		name    string
		oidc    OIDCConfig
		wantErr bool
	}{
		{
			name: "oidc complete is valid",
			oidc: OIDCConfig{
				Issuer:       "https://idp.example.com",
				ClientID:     "client",
				ClientSecret: "secret",
				RedirectURL:  "https://app.example.com/api/auth/callback",
			},
			wantErr: false,
		},
		{
			name:    "issuer only is invalid",
			oidc:    OIDCConfig{Issuer: "https://idp.example.com"},
			wantErr: true,
		},
		{
			name: "issuer without redirect_url is invalid",
			oidc: OIDCConfig{
				Issuer:       "https://idp.example.com",
				ClientID:     "client",
				ClientSecret: "secret",
			},
			wantErr: true,
		},
		{
			name:    "all empty is valid (dev mode)",
			oidc:    OIDCConfig{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validBase()
			c.Auth.OIDC = tt.oidc
			err := validate(c)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIsOIDCEnabled(t *testing.T) {
	tests := []struct {
		name string
		oidc OIDCConfig
		want bool
	}{
		{
			name: "all set is enabled",
			oidc: OIDCConfig{Issuer: "https://idp", ClientID: "c", ClientSecret: "s"},
			want: true,
		},
		{name: "all empty is disabled", oidc: OIDCConfig{}, want: false},
		{
			name: "issuer only is disabled",
			oidc: OIDCConfig{Issuer: "https://idp"},
			want: false,
		},
		{
			name: "missing client secret is disabled",
			oidc: OIDCConfig{Issuer: "https://idp", ClientID: "c"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validBase()
			c.Auth.OIDC = tt.oidc
			if got := c.IsOIDCEnabled(); got != tt.want {
				t.Errorf("IsOIDCEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}
