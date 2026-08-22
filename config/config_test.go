// SPDX-License-Identifier: CC0-1.0

package config_test

import (
	"testing"

	"github.com/m4schini/splitkauf/config"
)

const (
	testHost     = "localhost"
	testPassword = "secret"
)

func TestDatabaseConfigDSN(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  config.DatabaseConfig
		want string
	}{
		{
			name: "full config",
			cfg: config.DatabaseConfig{
				Host:     testHost,
				Port:     5432,
				User:     config.ServiceName,
				Password: testPassword,
				Name:     config.ServiceName,
				SSLMode:  "disable",
			},
			want: "host='localhost' port='5432' user='splitkauf' password='secret' dbname='splitkauf' sslmode='disable'",
		},
		{
			// An empty password must stay quoted so the parser does not swallow
			// the following dbname keyword into an unquoted empty value.
			name: "empty password stays quoted",
			cfg: config.DatabaseConfig{
				Host:     testHost,
				Port:     5432,
				User:     config.ServiceName,
				Password: "",
				Name:     config.ServiceName,
				SSLMode:  "disable",
			},
			want: "host='localhost' port='5432' user='splitkauf' password='' dbname='splitkauf' sslmode='disable'",
		},
		{
			name: "special characters are escaped",
			cfg: config.DatabaseConfig{
				Host:     testHost,
				Port:     5432,
				User:     "spl it",
				Password: `pa'ss\word`,
				Name:     config.ServiceName,
				SSLMode:  "require",
			},
			want: `host='localhost' port='5432' user='spl it' password='pa\'ss\\word' dbname='splitkauf' sslmode='require'`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.cfg.DSN(); got != tt.want {
				t.Errorf("DSN() =\n  %q\nwant\n  %q", got, tt.want)
			}
		})
	}
}
