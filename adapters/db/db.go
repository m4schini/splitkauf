// SPDX-License-Identifier: CC0-1.0

package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" with database/sql
)

const pingTimeout = 5 * time.Second

// NewSQL opens a *sql.DB backed by pgx (via database/sql) and verifies the
// connection with a PingContext. On ping failure it returns the opened handle
// *and* the error, so callers can decide how severe the failure is (serve
// starts degraded; migrate treats it as fatal).
func NewSQL(dsn string) (*sql.DB, error) {
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database handle: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()

	if err := conn.PingContext(ctx); err != nil {
		return conn, fmt.Errorf("pinging database: %w", err)
	}

	return conn, nil
}
