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

// ListsRepository is the Postgres implementation of lists.Repository over a
// *sql.DB. It uses plain parameterised SQL (no ORM) and maps the driver's
// no-rows sentinel to lists.ErrNotFound. Timestamps' updated_at columns are
// app-managed on writes (created_at falls back to the schema default).
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

// itemColumns is the column list for an item row, unqualified, for the INSERT
// and UPDATE statements that write one.
const itemColumns = `id, list_id, name, quantity, unit, note, checked, checked_at, created_at, updated_at`

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

// CreateList inserts a new, empty list credited to createdBy and returns it
// with zero counts. The row is re-read through List() rather than returned by
// RETURNING: the creator's display name comes from a join no RETURNING clause
// can perform (see listSelect).
func (r *ListsRepository) CreateList(ctx context.Context, name string, createdBy uuid.UUID) (lists.List, error) {
	now := time.Now()
	var id uuid.UUID
	if err := r.db.QueryRowContext(ctx,
		`INSERT INTO lists (name, created_by, created_at, updated_at) VALUES ($1, $2, $3, $3)
		 RETURNING id`,
		name, createdBy, now,
	).Scan(&id); err != nil {
		return lists.List{}, fmt.Errorf("create list: %w", err)
	}
	return r.List(ctx, id)
}

// Lists returns every list with its item-count summary, newest first.
func (r *ListsRepository) Lists(ctx context.Context) ([]lists.List, error) {
	rows, err := r.db.QueryContext(ctx,
		listSelect+` GROUP BY l.id, m.name ORDER BY l.created_at DESC, l.id`)
	if err != nil {
		return nil, fmt.Errorf("query lists: %w", err)
	}
	defer rows.Close()

	var result []lists.List
	for rows.Next() {
		l, err := scanList(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate lists: %w", err)
	}
	return result, nil
}

// List returns a single list with its counts, or lists.ErrNotFound.
func (r *ListsRepository) List(ctx context.Context, id uuid.UUID) (lists.List, error) {
	row := r.db.QueryRowContext(ctx,
		listSelect+` WHERE l.id = $1 GROUP BY l.id, m.name`, id)

	l, err := scanList(row)
	if errors.Is(err, sql.ErrNoRows) {
		return lists.List{}, lists.ErrNotFound
	}
	if err != nil {
		return lists.List{}, err
	}
	return l, nil
}

// ListItems returns all items on a list, open and checked, oldest first. It
// does not distinguish a missing list from an empty one (an empty slice).
func (r *ListsRepository) ListItems(ctx context.Context, listID uuid.UUID) ([]lists.Item, error) {
	rows, err := r.db.QueryContext(ctx,
		itemSelect+` WHERE i.list_id = $1 AND i.deleted_at IS NULL ORDER BY i.created_at, i.id`, listID)
	if err != nil {
		return nil, fmt.Errorf("query items: %w", err)
	}
	defer rows.Close()

	var result []lists.Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate items: %w", err)
	}
	return result, nil
}

// RenameList updates a list's name and returns the refreshed list (with
// counts), or lists.ErrNotFound when it does not exist.
func (r *ListsRepository) RenameList(ctx context.Context, id uuid.UUID, name string) (lists.List, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE lists SET name = $2, updated_at = $3 WHERE id = $1`,
		id, name, time.Now())
	if err != nil {
		return lists.List{}, fmt.Errorf("rename list: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return lists.List{}, lists.ErrNotFound
	}
	return r.List(ctx, id)
}

// DeleteList deletes a list; its items cascade via the FK. It returns
// lists.ErrNotFound when the list does not exist.
func (r *ListsRepository) DeleteList(ctx context.Context, id uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM lists WHERE id = $1`, id)
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
func (r *ListsRepository) CopyList(ctx context.Context, sourceID uuid.UUID, name string, actor uuid.UUID) (lists.List, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return lists.List{}, fmt.Errorf("copy list: begin: %w", err)
	}
	// Rolled back unless the commit below already ended the transaction, in
	// which case this is a no-op returning ErrTxDone.
	defer func() { _ = tx.Rollback() }()

	var exists int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM lists WHERE id = $1 FOR KEY SHARE`, sourceID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return lists.List{}, lists.ErrNotFound
	}
	if err != nil {
		return lists.List{}, fmt.Errorf("copy list: load source: %w", err)
	}

	now := time.Now()
	var newID uuid.UUID
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO lists (name, created_by, created_at, updated_at) VALUES ($1, $2, $3, $3)
		 RETURNING id`,
		name, actor, now,
	).Scan(&newID); err != nil {
		return lists.List{}, fmt.Errorf("copy list: insert list: %w", err)
	}

	// The copier is credited with adding every copied item, and nobody has
	// bought any of them yet — the copy starts as their fresh shopping list.
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO items (list_id, name, quantity, unit, note, checked, checked_at, added_by, bought_by, created_at, updated_at)
		 SELECT $1, name, quantity, unit, note, false, NULL, $4, NULL,
		        $3::timestamptz + row_number() OVER (ORDER BY created_at, id) * interval '1 microsecond',
		        $3
		 FROM items WHERE list_id = $2 AND deleted_at IS NULL`,
		newID, sourceID, now, actor); err != nil {
		return lists.List{}, fmt.Errorf("copy list: insert items: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return lists.List{}, fmt.Errorf("copy list: commit: %w", err)
	}

	// Re-read for the counts and the creator's display name, which the join in
	// listSelect resolves and no RETURNING clause could.
	return r.List(ctx, newID)
}

// AddItem inserts an item onto a list. It does not pre-check that the list
// exists: a missing list is detected by the insert's own foreign-key
// violation (SQLSTATE 23503), which is mapped to lists.ErrNotFound below.
// Pre-checking would open a TOCTOU race against a concurrent DeleteList
// between the check and the insert.
func (r *ListsRepository) AddItem(ctx context.Context, listID uuid.UUID, name string, quantity int, unit string, note *string, checked bool, addedBy uuid.UUID) (lists.Item, error) {
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
	var id uuid.UUID
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO items (list_id, name, quantity, unit, note, checked, checked_at, added_by, bought_by, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)
		 RETURNING id`,
		listID, name, quantity, unit, nullString(note), checked, checkedAt, addedBy, boughtBy, now,
	).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgErrForeignKeyViolation {
			return lists.Item{}, lists.ErrNotFound
		}
		return lists.Item{}, fmt.Errorf("add item: %w", err)
	}
	return r.Item(ctx, listID, id)
}

// Item returns a single item scoped to its list, or lists.ErrNotFound.
func (r *ListsRepository) Item(ctx context.Context, listID, itemID uuid.UUID) (lists.Item, error) {
	row := r.db.QueryRowContext(ctx,
		itemSelect+` WHERE i.id = $1 AND i.list_id = $2 AND i.deleted_at IS NULL`, itemID, listID)

	it, err := scanItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return lists.Item{}, lists.ErrNotFound
	}
	if err != nil {
		return lists.Item{}, err
	}
	return it, nil
}

// UpdateItem applies a partial update to an item and returns the updated row,
// or lists.ErrNotFound. Only fields present in update are written.
func (r *ListsRepository) UpdateItem(ctx context.Context, listID, itemID uuid.UUID, update lists.ItemUpdate) (lists.Item, error) {
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
func (r *ListsRepository) SetItemChecked(ctx context.Context, listID, itemID uuid.UUID, checked bool, checkedAt *time.Time, checkedBy *uuid.UUID) (lists.Item, error) {
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

// scanList scans a list projection (see listSelect) into a lists.List.
func scanList(s scanner) (lists.List, error) {
	var (
		l             lists.List
		createdBy     uuid.NullUUID
		createdByName sql.NullString
	)
	if err := s.Scan(&l.ID, &l.Name, &l.CreatedAt, &l.UpdatedAt, &l.OpenItemCount, &l.CheckedItemCount,
		&createdBy, &createdByName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return lists.List{}, err
		}
		return lists.List{}, fmt.Errorf("scan list: %w", err)
	}
	l.CreatedBy = toActor(createdBy, createdByName)
	return l, nil
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

// scanItem scans an item row (see itemColumns) into a lists.Item, translating
// the nullable note/checked_at columns to their pointer fields.
func scanItem(s scanner) (lists.Item, error) {
	var (
		it           lists.Item
		note         sql.NullString
		checkedAt    sql.NullTime
		addedBy      uuid.NullUUID
		addedByName  sql.NullString
		boughtBy     uuid.NullUUID
		boughtByName sql.NullString
	)
	if err := s.Scan(&it.ID, &it.ListID, &it.Name, &it.Quantity, &it.Unit, &note, &it.Checked, &checkedAt,
		&it.CreatedAt, &it.UpdatedAt, &addedBy, &addedByName, &boughtBy, &boughtByName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return lists.Item{}, err
		}
		return lists.Item{}, fmt.Errorf("scan item: %w", err)
	}
	if note.Valid {
		it.Note = &note.String
	}
	if checkedAt.Valid {
		t := checkedAt.Time
		it.CheckedAt = &t
	}
	it.AddedBy = toActor(addedBy, addedByName)
	it.BoughtBy = toActor(boughtBy, boughtByName)
	return it, nil
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
