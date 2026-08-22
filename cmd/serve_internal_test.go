// SPDX-License-Identifier: CC0-1.0

package cmd

import "testing"

func TestSessionStore(t *testing.T) {
	t.Parallel()

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
			wantErr:      "",
		},
		{
			name:         "oidc with unreachable db fails fast",
			oidcEnabled:  true,
			dbReachable:  false,
			wantPostgres: false,
			wantErr:      "sessions require a reachable database in OIDC mode",
		},
		{
			name:         "dev-auth with reachable db uses postgres",
			oidcEnabled:  false,
			dbReachable:  true,
			wantPostgres: true,
			wantErr:      "",
		},
		{
			name:         "dev-auth with unreachable db falls back to memory",
			oidcEnabled:  false,
			dbReachable:  false,
			wantPostgres: false,
			wantErr:      "",
		},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			usePostgres, err := sessionStore(tst.oidcEnabled, tst.dbReachable)

			if tst.wantErr != "" {
				if err == nil {
					t.Fatalf("sessionStore() error = nil, want %q", tst.wantErr)
				}

				if err.Error() != tst.wantErr {
					t.Fatalf("sessionStore() error = %q, want %q", err.Error(), tst.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("sessionStore() unexpected error = %v", err)
			}

			if usePostgres != tst.wantPostgres {
				t.Errorf("sessionStore() usePostgres = %v, want %v", usePostgres, tst.wantPostgres)
			}
		})
	}
}
