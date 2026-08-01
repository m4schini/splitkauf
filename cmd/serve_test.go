// SPDX-License-Identifier: TODO

package cmd

import "testing"

func TestSessionStore(t *testing.T) {
	tests := []struct {
		name         string
		oidcEnabled  bool
		dbReachable  bool
		wantPostgres bool
		wantErr      string
	}{
		{
			name:         "oidc with reachable db uses postgres",
			oidcEnabled:  true,
			dbReachable:  true,
			wantPostgres: true,
		},
		{
			name:        "oidc with unreachable db fails fast",
			oidcEnabled: true,
			dbReachable: false,
			wantErr:     "sessions require a reachable database in OIDC mode",
		},
		{
			name:         "dev-auth with reachable db uses postgres",
			oidcEnabled:  false,
			dbReachable:  true,
			wantPostgres: true,
		},
		{
			name:         "dev-auth with unreachable db falls back to memory",
			oidcEnabled:  false,
			dbReachable:  false,
			wantPostgres: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usePostgres, err := sessionStore(tt.oidcEnabled, tt.dbReachable)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("sessionStore() error = nil, want %q", tt.wantErr)
				}
				if err.Error() != tt.wantErr {
					t.Fatalf("sessionStore() error = %q, want %q", err.Error(), tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("sessionStore() unexpected error = %v", err)
			}
			if usePostgres != tt.wantPostgres {
				t.Errorf("sessionStore() usePostgres = %v, want %v", usePostgres, tt.wantPostgres)
			}
		})
	}
}
