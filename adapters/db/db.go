// SPDX-License-Identifier: TODO

package db

import (
	"context"
	"database/sql"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" with database/sql
)

const pingTimeout = 5 * time.Second

// NewSQL opens a *sql.DB backed by pgx (via database/sql) and verifies the
// connection with a PingContext. On ping failure it returns the opened handle
// *and* the error, so callers can decide how severe the failure is (serve
// starts degraded; migrate treats it as fatal).
func NewSQL(dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return db, err
	}
	return db, nil
}
