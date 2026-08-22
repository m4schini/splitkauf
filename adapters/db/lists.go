// SPDX-License-Identifier: CC0-1.0

package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/m4schini/splitkauf/lists"
)

// ListsRepository is the Postgres implementation of lists.Repository over a
// *sql.DB. It uses plain parameterised SQL (no ORM) and maps the driver's
// no-rows sentinel to lists.ErrNotFound. Timestamps' updated_at columns are
// app-managed on writes (created_at falls back to the schema default).
//
// The list-level methods live in this file; the item-level methods and the
// shared row-scanning helpers live in items.go.
type ListsRepository struct {
	db *sql.DB
}

// NewListsRepository constructs a ListsRepository over the given handle.
func NewListsRepository(db *sql.DB) *ListsRepository {
	return &ListsRepository{db: db}
}

// Ensure ListsRepository satisfies the domain port at compile time.
var _ lists.Repository = (*ListsRepository)(nil)

// listSelect is the projection for a list together with its open/checked item
// counts, derived by a left join over items. It is shared by the single-list
// and all-lists reads so both compute counts identically. The soft-delete
// filter (deleted_at IS NULL) lives in the JOIN's ON clause, NOT a WHERE: a
// WHERE would demote the LEFT JOIN to an inner join and drop lists whose items
// are all deleted (and empty lists) from the results.
// The creator's display name is resolved here rather than stored, so a member
// rename propagates to every list they ever created. The join is on the unique
// members.user_id, so it matches at most one row and cannot multiply the item
// rows the counts aggregate over. A creator with no member row (or a list from
// before attribution) simply yields NULLs.
const listSelect = `
	SELECT l.id, l.name, l.created_at, l.updated_at,
	       COALESCE(SUM(CASE WHEN i.checked = false THEN 1 ELSE 0 END), 0) AS open_count,
	       COALESCE(SUM(CASE WHEN i.checked = true  THEN 1 ELSE 0 END), 0) AS checked_count,
	       l.created_by, m.name AS created_by_name
	FROM lists l
	LEFT JOIN items i ON i.list_id = l.id AND i.deleted_at IS NULL
	LEFT JOIN members m ON m.user_id = l.created_by`

// CreateList inserts a new, empty list credited to createdBy and returns it
// with zero counts. The row is re-read through List() rather than returned by
// RETURNING: the creator's display name comes from a join no RETURNING clause
// can perform (see listSelect).
func (r *ListsRepository) CreateList(ctx context.Context, name string, createdBy uuid.UUID) (lists.List, error) {
	now := time.Now()

	var listID uuid.UUID
	if err := r.db.QueryRowContext(ctx,
		`INSERT INTO lists (name, created_by, created_at, updated_at) VALUES ($1, $2, $3, $3)
		 RETURNING id`,
		name, createdBy, now,
	).Scan(&listID); err != nil {
		return lists.List{}, fmt.Errorf("create list: %w", err)
	}

	return r.List(ctx, listID)
}

// Lists returns every list with its item-count summary, newest first.
func (r *ListsRepository) Lists(ctx context.Context) ([]lists.List, error) {
	rows, err := r.db.QueryContext(ctx,
		listSelect+` GROUP BY l.id, m.name ORDER BY l.created_at DESC, l.id`)
	if err != nil {
		return nil, fmt.Errorf("query lists: %w", err)
	}

	defer func() { _ = rows.Close() }()

	var result []lists.List

	for rows.Next() {
		list, err := scanList(rows)
		if err != nil {
			return nil, err
		}

		result = append(result, list)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate lists: %w", err)
	}

	return result, nil
}

// List returns a single list with its counts, or lists.ErrNotFound.
func (r *ListsRepository) List(ctx context.Context, listID uuid.UUID) (lists.List, error) {
	row := r.db.QueryRowContext(ctx,
		listSelect+` WHERE l.id = $1 GROUP BY l.id, m.name`, listID)

	list, err := scanList(row)
	if errors.Is(err, sql.ErrNoRows) {
		return lists.List{}, lists.ErrNotFound
	}

	if err != nil {
		return lists.List{}, err
	}

	return list, nil
}

// RenameList updates a list's name and returns the refreshed list (with
// counts), or lists.ErrNotFound when it does not exist.
func (r *ListsRepository) RenameList(ctx context.Context, listID uuid.UUID, name string) (lists.List, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE lists SET name = $2, updated_at = $3 WHERE id = $1`,
		listID, name, time.Now())
	if err != nil {
		return lists.List{}, fmt.Errorf("rename list: %w", err)
	}

	if n, _ := res.RowsAffected(); n == 0 {
		return lists.List{}, lists.ErrNotFound
	}

	return r.List(ctx, listID)
}

// DeleteList deletes a list; its items cascade via the FK. It returns
// lists.ErrNotFound when the list does not exist.
func (r *ListsRepository) DeleteList(ctx context.Context, listID uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM lists WHERE id = $1`, listID)
	if err != nil {
		return fmt.Errorf("delete list: %w", err)
	}

	if n, _ := res.RowsAffected(); n == 0 {
		return lists.ErrNotFound
	}

	return nil
}

// CopyList creates a new list named name holding a copy of every non-deleted
// item of the source list, each reset to unchecked. It is the repository's only
// transactional method: the list row and its items are written together so a
// failure never leaves a half-populated copy behind.
//
// The source row is re-read inside the transaction with FOR KEY SHARE. That
// both provides the not-found guard and locks the row, so a concurrent
// DeleteList cannot commit between the guard and the item copy (which would
// otherwise yield a silently empty copy).
//
// Copied items are stamped with staggered created_at values (one microsecond
// apart, in the source's display order): item order everywhere is
// ORDER BY created_at, id, so a single shared timestamp would leave the copy's
// order to the UUID tie-break, while reusing the source timestamps would make
// the copied items look older than the list holding them.
func (r *ListsRepository) CopyList(
	ctx context.Context, sourceID uuid.UUID, name string, actor uuid.UUID,
) (lists.List, error) {
	transaction, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return lists.List{}, fmt.Errorf("copy list: begin: %w", err)
	}
	// Rolled back unless the commit below already ended the transaction, in
	// which case this is a no-op returning ErrTxDone.
	defer func() { _ = transaction.Rollback() }()

	var exists int

	err = transaction.QueryRowContext(ctx,
		`SELECT 1 FROM lists WHERE id = $1 FOR KEY SHARE`, sourceID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return lists.List{}, lists.ErrNotFound
	}

	if err != nil {
		return lists.List{}, fmt.Errorf("copy list: load source: %w", err)
	}

	now := time.Now()

	var newID uuid.UUID
	if err := transaction.QueryRowContext(ctx,
		`INSERT INTO lists (name, created_by, created_at, updated_at) VALUES ($1, $2, $3, $3)
		 RETURNING id`,
		name, actor, now,
	).Scan(&newID); err != nil {
		return lists.List{}, fmt.Errorf("copy list: insert list: %w", err)
	}

	// The copier is credited with adding every copied item, and nobody has
	// bought any of them yet — the copy starts as their fresh shopping list.
	if _, err := transaction.ExecContext(ctx,
		`INSERT INTO items
		     (list_id, name, quantity, unit, note, checked, checked_at, added_by, bought_by, created_at, updated_at)
		 SELECT $1, name, quantity, unit, note, false, NULL, $4, NULL,
		        $3::timestamptz + row_number() OVER (ORDER BY created_at, id) * interval '1 microsecond',
		        $3
		 FROM items WHERE list_id = $2 AND deleted_at IS NULL`,
		newID, sourceID, now, actor); err != nil {
		return lists.List{}, fmt.Errorf("copy list: insert items: %w", err)
	}

	if err := transaction.Commit(); err != nil {
		return lists.List{}, fmt.Errorf("copy list: commit: %w", err)
	}

	// Re-read for the counts and the creator's display name, which the join in
	// listSelect resolves and no RETURNING clause could.
	return r.List(ctx, newID)
}

// scanList scans a list projection (see listSelect) into a lists.List. The
// driver's sql.ErrNoRows stays recognisable through the %w wrap, so callers
// keep mapping it to lists.ErrNotFound with errors.Is.
func scanList(row scanner) (lists.List, error) {
	var (
		list          lists.List
		createdBy     uuid.NullUUID
		createdByName sql.NullString
	)
	if err := row.Scan(&list.ID, &list.Name, &list.CreatedAt, &list.UpdatedAt,
		&list.OpenItemCount, &list.CheckedItemCount,
		&createdBy, &createdByName); err != nil {
		return lists.List{}, fmt.Errorf("scan list: %w", err)
	}

	list.CreatedBy = toActor(createdBy, createdByName)

	return list, nil
}
