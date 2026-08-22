// SPDX-License-Identifier: CC0-1.0

package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/m4schini/splitkauf/lists"
)

// pgErrForeignKeyViolation is the Postgres SQLSTATE for a foreign-key
// violation. AddItem maps it to lists.ErrNotFound.
const pgErrForeignKeyViolation = "23503"

// itemSelect is the projection for reading an item, ordered to match scanItem.
// Like listSelect it resolves the attributions' display names through members
// rather than storing them, so a rename applies retroactively. Two separate
// joins are needed because the adder and the buyer are independent people.
const itemSelect = `
	SELECT i.id, i.list_id, i.name, i.quantity, i.unit, i.note, i.checked, i.checked_at,
	       i.created_at, i.updated_at,
	       i.added_by, ma.name AS added_by_name,
	       i.bought_by, mb.name AS bought_by_name
	FROM items i
	LEFT JOIN members ma ON ma.user_id = i.added_by
	LEFT JOIN members mb ON mb.user_id = i.bought_by`

// ListItems returns all items on a list, open and checked, oldest first. It
// does not distinguish a missing list from an empty one (an empty slice).
func (r *ListsRepository) ListItems(ctx context.Context, listID uuid.UUID) ([]lists.Item, error) {
	rows, err := r.db.QueryContext(ctx,
		itemSelect+` WHERE i.list_id = $1 AND i.deleted_at IS NULL ORDER BY i.created_at, i.id`, listID)
	if err != nil {
		return nil, fmt.Errorf("query items: %w", err)
	}

	defer func() { _ = rows.Close() }()

	var result []lists.Item

	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, err
		}

		result = append(result, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate items: %w", err)
	}

	return result, nil
}

// AddItem inserts an item onto a list. It does not pre-check that the list
// exists: a missing list is detected by the insert's own foreign-key
// violation (SQLSTATE 23503), which is mapped to lists.ErrNotFound below.
// Pre-checking would open a TOCTOU race against a concurrent DeleteList
// between the check and the insert.
func (r *ListsRepository) AddItem(
	ctx context.Context,
	listID uuid.UUID,
	name string,
	quantity int,
	unit string,
	note *string,
	checked bool,
	addedBy uuid.UUID,
) (lists.Item, error) {
	now := time.Now()
	// When the item is created already checked (an offline check folded into a
	// queued create), stamp checked_at at insert; otherwise leave it NULL. The
	// buyer follows the same rule: that fold means the adder checked it off
	// themselves before it ever reached the server, and no SetItemChecked will
	// follow to record who did.
	var (
		checkedAt any
		boughtBy  any
	)
	if checked {
		checkedAt = now
		boughtBy = addedBy
	}

	var itemID uuid.UUID

	err := r.db.QueryRowContext(ctx,
		`INSERT INTO items
		     (list_id, name, quantity, unit, note, checked, checked_at, added_by, bought_by, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)
		 RETURNING id`,
		listID, name, quantity, unit, nullString(note), checked, checkedAt, addedBy, boughtBy, now,
	).Scan(&itemID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgErrForeignKeyViolation {
			return lists.Item{}, lists.ErrNotFound
		}

		return lists.Item{}, fmt.Errorf("add item: %w", err)
	}

	return r.Item(ctx, listID, itemID)
}

// Item returns a single item scoped to its list, or lists.ErrNotFound.
func (r *ListsRepository) Item(ctx context.Context, listID, itemID uuid.UUID) (lists.Item, error) {
	row := r.db.QueryRowContext(ctx,
		itemSelect+` WHERE i.id = $1 AND i.list_id = $2 AND i.deleted_at IS NULL`, itemID, listID)

	item, err := scanItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return lists.Item{}, lists.ErrNotFound
	}

	if err != nil {
		return lists.Item{}, err
	}

	return item, nil
}

// UpdateItem applies a partial update to an item and returns the updated row,
// or lists.ErrNotFound. Only fields present in update are written.
func (r *ListsRepository) UpdateItem(
	ctx context.Context, listID, itemID uuid.UUID, update lists.ItemUpdate,
) (lists.Item, error) {
	sets := []string{"updated_at = $1"}
	args := []any{time.Now()}
	next := 2

	if update.Name != nil {
		sets = append(sets, fmt.Sprintf("name = $%d", next))
		args = append(args, *update.Name)
		next++
	}

	if update.Quantity != nil {
		sets = append(sets, fmt.Sprintf("quantity = $%d", next))
		args = append(args, *update.Quantity)
		next++
	}

	if update.Unit != nil {
		sets = append(sets, fmt.Sprintf("unit = $%d", next))
		args = append(args, *update.Unit)
		next++
	}

	if update.NoteSet {
		sets = append(sets, fmt.Sprintf("note = $%d", next))
		args = append(args, nullString(update.Note))
		next++
	}

	// The interpolated parts are not caller-controlled: sets holds fixed
	// "column = $n" fragments chosen by the switch above, and the indexes are
	// counters. Every caller-supplied value is a bound parameter in args.
	query := fmt.Sprintf( //nolint:gosec // G201: only internal fragments are interpolated; values are bound
		`UPDATE items SET %s WHERE id = $%d AND list_id = $%d AND deleted_at IS NULL`,
		strings.Join(sets, ", "), next, next+1)

	args = append(args, itemID, listID)

	res, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return lists.Item{}, fmt.Errorf("update item: %w", err)
	}

	if n, _ := res.RowsAffected(); n == 0 {
		return lists.Item{}, lists.ErrNotFound
	}
	// Re-read so the response carries the attributions with their names
	// resolved; editing an item never changes who added or bought it.
	return r.Item(ctx, listID, itemID)
}

// DeleteItem soft-deletes an item by stamping deleted_at: the row is kept so the
// delete can be replayed offline and undone via RestoreItem. An already-deleted
// or missing item yields lists.ErrNotFound (0 rows affected).
func (r *ListsRepository) DeleteItem(ctx context.Context, listID, itemID uuid.UUID) error {
	now := time.Now()

	res, err := r.db.ExecContext(ctx,
		`UPDATE items SET deleted_at = $3, updated_at = $3
		 WHERE id = $1 AND list_id = $2 AND deleted_at IS NULL`, itemID, listID, now)
	if err != nil {
		return fmt.Errorf("delete item: %w", err)
	}

	if n, _ := res.RowsAffected(); n == 0 {
		return lists.ErrNotFound
	}

	return nil
}

// RestoreItem clears a soft delete and returns the restored item. It is
// idempotent: restoring an item that is not deleted succeeds and returns it
// unchanged. A missing row yields lists.ErrNotFound. It mirrors
// SetItemChecked's write-and-return shape.
func (r *ListsRepository) RestoreItem(ctx context.Context, listID, itemID uuid.UUID) (lists.Item, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE items SET deleted_at = NULL, updated_at = $3
		 WHERE list_id = $1 AND id = $2`,
		listID, itemID, time.Now())
	if err != nil {
		return lists.Item{}, fmt.Errorf("restore item: %w", err)
	}

	if n, _ := res.RowsAffected(); n == 0 {
		return lists.Item{}, lists.ErrNotFound
	}
	// A restored item keeps the attributions it had before the delete; the
	// re-read is what resolves their names (see itemSelect).
	return r.Item(ctx, listID, itemID)
}

// SetItemChecked writes an item's checked state, checkedAt timestamp and buyer,
// returning the updated row or lists.ErrNotFound. A nil checkedBy clears the
// buyer, which is how unchecking leaves no stale "bought by" behind.
func (r *ListsRepository) SetItemChecked(
	ctx context.Context,
	listID, itemID uuid.UUID,
	checked bool,
	checkedAt *time.Time,
	checkedBy *uuid.UUID,
) (lists.Item, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE items SET checked = $3, checked_at = $4, bought_by = $5, updated_at = $6
		 WHERE id = $1 AND list_id = $2 AND deleted_at IS NULL`,
		itemID, listID, checked, nullTime(checkedAt), nullUUID(checkedBy), time.Now())
	if err != nil {
		return lists.Item{}, fmt.Errorf("set item checked: %w", err)
	}

	if n, _ := res.RowsAffected(); n == 0 {
		return lists.Item{}, lists.ErrNotFound
	}

	return r.Item(ctx, listID, itemID)
}

// scanner abstracts *sql.Row and *sql.Rows so scanList/scanItem serve both the
// single-row and iterating reads.
type scanner interface {
	Scan(dest ...any) error
}

// toActor builds the attribution for one (id, joined name) pair. A NULL id
// means unattributed (nil); a NULL name means the id has no member row, which
// is still worth reporting — the client recognises its own id without a name.
func toActor(id uuid.NullUUID, name sql.NullString) *lists.Actor {
	if !id.Valid {
		return nil
	}

	return &lists.Actor{ID: id.UUID, Name: name.String}
}

// scanItem scans an item row (see itemSelect) into a lists.Item, translating
// the nullable note/checked_at columns to their pointer fields. The driver's
// sql.ErrNoRows stays recognisable through the %w wrap, so callers keep
// mapping it to lists.ErrNotFound with errors.Is.
func scanItem(row scanner) (lists.Item, error) {
	var (
		item         lists.Item
		note         sql.NullString
		checkedAt    sql.NullTime
		addedBy      uuid.NullUUID
		addedByName  sql.NullString
		boughtBy     uuid.NullUUID
		boughtByName sql.NullString
	)
	if err := row.Scan(&item.ID, &item.ListID, &item.Name, &item.Quantity, &item.Unit, &note,
		&item.Checked, &checkedAt,
		&item.CreatedAt, &item.UpdatedAt, &addedBy, &addedByName, &boughtBy, &boughtByName); err != nil {
		return lists.Item{}, fmt.Errorf("scan item: %w", err)
	}

	if note.Valid {
		item.Note = &note.String
	}

	if checkedAt.Valid {
		t := checkedAt.Time
		item.CheckedAt = &t
	}

	item.AddedBy = toActor(addedBy, addedByName)
	item.BoughtBy = toActor(boughtBy, boughtByName)

	return item, nil
}

// nullString maps an optional string to a driver argument: nil becomes SQL NULL.
func nullString(s *string) any {
	if s == nil {
		return nil
	}

	return *s
}

// nullTime maps an optional time to a driver argument: nil becomes SQL NULL.
func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}

	return *t
}

// nullUUID maps an optional uuid to a driver argument: nil becomes SQL NULL.
func nullUUID(id *uuid.UUID) any {
	if id == nil {
		return nil
	}

	return *id
}
