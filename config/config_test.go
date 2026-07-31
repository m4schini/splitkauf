// SPDX-License-Identifier: TODO

package config

import "testing"

func TestDatabaseConfigDSN(t *testing.T) {
	tests := []struct {
		name string
		cfg  DatabaseConfig
		want string
	}{
		{
			name: "full config",
			cfg: DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				User:     "splitkauf",
				Password: "secret",
				Name:     "splitkauf",
				SSLMode:  "disable",
			},
			want: "host='localhost' port='5432' user='splitkauf' password='secret' dbname='splitkauf' sslmode='disable'",
		},
		{
			// An empty password must stay quoted so the parser does not swallow
			// the following dbname keyword into an unquoted empty value.
			name: "empty password stays quoted",
			cfg: DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				User:     "splitkauf",
				Password: "",
				Name:     "splitkauf",
				SSLMode:  "disable",
			},
			want: "host='localhost' port='5432' user='splitkauf' password='' dbname='splitkauf' sslmode='disable'",
		},
		{
			name: "special characters are escaped",
			cfg: DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				User:     "spl it",
				Password: `pa'ss\word`,
				Name:     "splitkauf",
				SSLMode:  "require",
			},
			want: `host='localhost' port='5432' user='spl it' password='pa\'ss\\word' dbname='splitkauf' sslmode='require'`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.DSN(); got != tt.want {
				t.Errorf("DSN() =\n  %q\nwant\n  %q", got, tt.want)
			}
		})
	}
}
