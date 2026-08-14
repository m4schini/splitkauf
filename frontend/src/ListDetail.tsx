import { useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, Copy, Pencil, Trash2 } from 'lucide-react'
import { attributionLabel, type Item, type List, type Unit, unitLabels, units } from './api'
import { Snackbar } from './Snackbar'
import { useUndoQueue } from './useUndoQueue'
import {
  listKey,
  listsKey,
  removeItemLocally,
  removeListLocally,
  restoreListLocally,
  useAddItem,
  useCheckItem,
  useCopyList,
  useDeleteItemMutation,
  useDeleteListMutation,
  useListDetail,
  useRenameList,
  useRestoreItem,
  useUncheckItem,
  useUpdateItem,
  useMe,
} from './queries'
import { useLiveEvents } from './live'

/**
 * An unsynced create keeps its `temp-` prefixed id (minted by `randomId`) until
 * its POST succeeds and reconciles to the server id — a marker that survives a
 * reload, since the persisted temp item keeps its temp id.
 */
function isUnsynced(id: string): boolean {
  return id.startsWith('temp-')
}

/**
 * The compact quantity+unit label shown next to an item's name. For the default
 * `amount` unit it renders the bare number ("3") and is omitted entirely for a
 * single item, so a plain name-only add stays uncluttered. For every other unit
 * the quantity and German label are shown ("2 l", "500 g", "2 Packung").
 * Returns null when there is nothing worth showing.
 */
function formatQuantity(quantity: number, unit: Unit): string | null {
  if (unit === 'amount') {
    return quantity > 1 ? String(quantity) : null
  }
  return `${quantity} ${unitLabels[unit]}`
}

interface ListDetailProps {
  listId: string
  onBack: () => void
  onDeleted: () => void
  /** Navigates into a freshly created copy of this list (US-L.10). */
  onCopied: (listId: string) => void
}

interface ItemRowProps {
  item: Item
  /** The viewer's user id, for rendering their own actions as "you". */
  meId: string | undefined
  unsynced: boolean
  onToggle: (item: Item) => void
  onEdit: (item: Item) => void
  onRemove: (item: Item) => void
  editing: boolean
  onSaveEdit: (name: string, quantity: number, unit: Unit, note: string) => void
  onCancelEdit: () => void
}

function ItemRow({
  item,
  meId,
  unsynced,
  onToggle,
  onEdit,
  onRemove,
  editing,
  onSaveEdit,
  onCancelEdit,
}: ItemRowProps) {
  const [name, setName] = useState(item.name)
  const [quantity, setQuantity] = useState(item.quantity)
  const [unit, setUnit] = useState<Unit>(item.unit)
  const [note, setNote] = useState(item.note ?? '')

  if (editing) {
    return (
      <li className="row item-row item-row-editing">
        <form
          className="item-edit-form"
          onSubmit={(e) => {
            e.preventDefault()
            onSaveEdit(name, quantity, unit, note)
          }}
        >
          <label htmlFor={`edit-name-${item.id}`}>Item name</label>
          <input
            id={`edit-name-${item.id}`}
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            autoFocus
          />
          <label htmlFor={`edit-qty-${item.id}`}>Quantity</label>
          <input
            id={`edit-qty-${item.id}`}
            type="number"
            min={1}
            value={quantity}
            onChange={(e) => setQuantity(Number(e.target.value) || 1)}
          />
          <label htmlFor={`edit-unit-${item.id}`}>Unit</label>
          <select
            id={`edit-unit-${item.id}`}
            value={unit}
            onChange={(e) => setUnit(e.target.value as Unit)}
          >
            {units.map((u) => (
              <option key={u} value={u}>
                {unitLabels[u]}
              </option>
            ))}
          </select>
          <label htmlFor={`edit-note-${item.id}`}>Note</label>
          <input
            id={`edit-note-${item.id}`}
            type="text"
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder="Optional note"
          />
          <div className="item-edit-actions">
            <button type="submit" className="primary-button">
              Save
            </button>
            <button type="button" className="secondary-button" onClick={onCancelEdit}>
              Cancel
            </button>
          </div>
        </form>
      </li>
    )
  }

  // A checked item reports who bought it; an open one who put it on the list.
  // Either can be unknown (an item from before attribution, or a member whose
  // name never resolved), in which case the line is left out entirely.
  const attributedTo = attributionLabel(item.checked ? item.boughtBy : item.addedBy, meId)
  const attribution = attributedTo && `${item.checked ? 'Bought' : 'Added'} by ${attributedTo}`

  return (
    <li className="row item-row">
      <label className="row-tap-target item-row-label">
        <input
          type="checkbox"
          className="item-checkbox"
          checked={item.checked}
          onChange={() => onToggle(item)}
        />
        <span className="item-text">
          <span className="item-name">{item.name}</span>
          {formatQuantity(item.quantity, item.unit) && (
            <span className="item-qty">{formatQuantity(item.quantity, item.unit)}</span>
          )}
          {item.note && <span className="item-note">{item.note}</span>}
          {attribution && <span className="item-attribution">{attribution}</span>}
          {unsynced && (
            <span className="unsynced-badge">
              <span className="unsynced-tag">Unsynced</span>
              <span className="unsynced-note">Needs internet to save</span>
            </span>
          )}
        </span>
      </label>
      <div className="item-row-actions">
        <button
          type="button"
          className="icon-button"
          aria-label={`Edit ${item.name}`}
          onClick={(e) => {
            e.preventDefault()
            e.stopPropagation()
            onEdit(item)
          }}
        >
          <Pencil size={20} aria-hidden="true" />
        </button>
        <button
          type="button"
          className="icon-button danger"
          aria-label={`Remove ${item.name}`}
          onClick={(e) => {
            e.preventDefault()
            e.stopPropagation()
            onRemove(item)
          }}
        >
          <Trash2 size={20} aria-hidden="true" />
        </button>
      </div>
    </li>
  )
}

/** List detail: open/done item sections, bottom quick-add, copy/rename/delete list. */
export function ListDetail({ listId, onBack, onDeleted, onCopied }: ListDetailProps) {
  const { data: list, isLoading, isError } = useListDetail(listId)
  const { data: me } = useMe()
  const addItem = useAddItem(listId)
  const updateItem = useUpdateItem(listId)
  const checkItem = useCheckItem(listId)
  const uncheckItem = useUncheckItem(listId)
  const renameList = useRenameList(listId)
  const copyList = useCopyList(listId)
  const deleteListMutation = useDeleteListMutation()
  const deleteItemMutation = useDeleteItemMutation(listId)
  const restoreItemMutation = useRestoreItem(listId)
  const queryClient = useQueryClient()
  const { pending, schedule, undo } = useUndoQueue()

  // Live updates (M3/US-S.1): an `items` event for this list means another
  // client added/edited/checked/removed an item — refetch this list's detail
  // (and the overview, for its open/checked counts). A `reconnect` may have
  // missed hints while disconnected, so it also does a full reload; a plain
  // `lists` event (rename/create/delete of some other list) doesn't affect
  // this screen and is ignored.
  useLiveEvents((event) => {
    if (event.type === 'items' && event.listId !== listId) return
    if (event.type === 'lists') return
    queryClient.invalidateQueries({ queryKey: listKey(listId) })
    queryClient.invalidateQueries({ queryKey: listsKey })
  })

  const [itemName, setItemName] = useState('')
  const [itemQuantity, setItemQuantity] = useState(1)
  const [itemUnit, setItemUnit] = useState<Unit>('amount')
  const itemInputRef = useRef<HTMLInputElement>(null)
  const [editingItemId, setEditingItemId] = useState<string | null>(null)
  const [renaming, setRenaming] = useState(false)
  const [nameDraft, setNameDraft] = useState('')

  function handleAddItem(e: React.FormEvent) {
    e.preventDefault()
    const trimmed = itemName.trim()
    if (!trimmed) return
    addItem.mutate({ name: trimmed, quantity: itemQuantity, unit: itemUnit })
    setItemName('')
    // Reset the additive controls so the next add defaults to 1 × amount — the
    // name-only quick-add stays exactly as fast (these never need touching).
    setItemQuantity(1)
    setItemUnit('amount')
    // Keep focus + keyboard open so "milk↵ eggs↵ bread↵" chains without
    // the user re-tapping the field.
    itemInputRef.current?.focus()
  }

  function handleToggle(item: Item) {
    if (item.checked) {
      uncheckItem.mutate(item.id)
    } else {
      checkItem.mutate(item.id)
    }
  }

  async function handleRemoveItem(item: Item) {
    await queryClient.cancelQueries({ queryKey: listKey(listId) })
    // A still-queued offline create is cancelled outright by the delete
    // mutation (US-O.2 Key Decision 3) — undo therefore re-adds it as a fresh
    // create rather than calling the server-side restore on an id the server
    // never saw.
    const wasPendingCreate = isUnsynced(item.id)
    removeItemLocally(queryClient, listId, item)
    // Item delete is a server-backed soft delete (US-O.2 Key Decision 4): the
    // mutation fires immediately rather than waiting out the undo window, so
    // it works offline like any other mutation. The undo snackbar's action
    // restores it server-side via useRestoreItem rather than cancelling a
    // deferred delete — the `commit` callback below is a no-op because the
    // delete already happened.
    deleteItemMutation.mutate(item.id)
    schedule(
      item.id,
      `Removed "${item.name}" — Undo`,
      () => {},
      () => {
        if (wasPendingCreate) {
          addItem.mutate({
            name: item.name,
            quantity: item.quantity,
            note: item.note ?? null,
            checked: item.checked,
          })
        } else {
          restoreItemMutation.mutate(item)
        }
      },
    )
  }

  async function handleDeleteList() {
    await queryClient.cancelQueries({ queryKey: listsKey })
    const cachedLists = queryClient.getQueryData<List[]>(listsKey)
    const index = cachedLists?.findIndex((l) => l.id === listId) ?? -1
    // Fall back to the detail-screen's own summary fields if the overview
    // cache doesn't have this list yet (e.g. deep-linked straight here), so
    // undo still has something to re-insert.
    const summary: List =
      index >= 0 && cachedLists
        ? cachedLists[index]
        : {
            id: listId,
            name: list?.name ?? '',
            openItemCount: list?.openItemCount ?? 0,
            checkedItemCount: list?.checkedItemCount ?? 0,
            createdAt: list?.createdAt ?? new Date().toISOString(),
            updatedAt: list?.updatedAt ?? new Date().toISOString(),
          }
    removeListLocally(queryClient, summary)
    schedule(
      listId,
      `Deleted "${list?.name ?? 'list'}" — Undo`,
      () => {
        deleteListMutation.mutate(listId)
        onDeleted()
      },
      () => {
        restoreListLocally(queryClient, summary, index)
      },
    )
  }

  // Copy is online-only (US-L.10): it is never queued, so a failure (typically
  // being offline) surfaces as the inline hint below the header rather than
  // blocking the screen. A retry clears the error via the mutation's own state.
  function handleCopyList() {
    copyList.mutate(undefined, { onSuccess: (created) => onCopied(created.id) })
  }

  function startRename() {
    setNameDraft(list?.name ?? '')
    setRenaming(true)
  }

  function saveRename(e: React.FormEvent) {
    e.preventDefault()
    const trimmed = nameDraft.trim()
    if (trimmed && trimmed !== list?.name) {
      renameList.mutate(trimmed)
    }
    setRenaming(false)
  }

  if (isLoading) {
    return (
      <div className="screen">
        <p className="hint">Loading…</p>
      </div>
    )
  }

  if (isError || !list) {
    return (
      <div className="screen">
        <p className="hint hint-error">Couldn't load this list.</p>
        <button type="button" className="secondary-button" onClick={onBack}>
          Back to lists
        </button>
      </div>
    )
  }

  const listPendingDelete = pending.some((p) => p.id === listId)
  if (listPendingDelete) {
    return (
      <div className="screen">
        <p className="hint">List deleted.</p>
        <Snackbar entries={pending} onUndo={undo} />
      </div>
    )
  }

  const visibleItems = list.items.filter((i) => !pending.some((p) => p.id === i.id))
  const openItems = visibleItems.filter((i) => !i.checked)
  const doneItems = visibleItems.filter((i) => i.checked)

  return (
    <div className="screen">
      <header className="screen-header">
        <button type="button" className="icon-button" onClick={onBack} aria-label="Back to lists">
          <ArrowLeft size={20} aria-hidden="true" />
        </button>
        {renaming ? (
          <form className="rename-form" onSubmit={saveRename}>
            <label htmlFor="rename-list">List name</label>
            <input
              id="rename-list"
              type="text"
              value={nameDraft}
              onChange={(e) => setNameDraft(e.target.value)}
              autoFocus
              onBlur={saveRename}
            />
          </form>
        ) : (
          <h1 className="list-title">{list.name}</h1>
        )}
        <div className="screen-header-actions">
          <button
            type="button"
            className="icon-button"
            onClick={handleCopyList}
            aria-label="Copy list"
            disabled={copyList.isPending}
          >
            <Copy size={20} aria-hidden="true" />
          </button>
          <button
            type="button"
            className="icon-button"
            onClick={startRename}
            aria-label="Rename list"
          >
            <Pencil size={20} aria-hidden="true" />
          </button>
          <button
            type="button"
            className="icon-button danger"
            onClick={handleDeleteList}
            aria-label="Delete list"
          >
            <Trash2 size={20} aria-hidden="true" />
          </button>
        </div>
      </header>

      {copyList.isError && <p className="hint hint-error">Couldn't copy the list.</p>}

      <div className="scroll-area">
        <section aria-labelledby="open-heading">
          <h2 id="open-heading" className="section-heading">
            Open ({openItems.length})
          </h2>
          {openItems.length === 0 && <p className="hint">Nothing to buy yet.</p>}
          <ul className="row-list">
            {openItems.map((item) => (
              <ItemRow
                key={item.id}
                item={item}
                meId={me?.id}
                unsynced={isUnsynced(item.id)}
                onToggle={handleToggle}
                onEdit={(i) => setEditingItemId(i.id)}
                onRemove={handleRemoveItem}
                editing={editingItemId === item.id}
                onSaveEdit={(name, quantity, unit, note) => {
                  updateItem.mutate({
                    itemId: item.id,
                    body: { name, quantity, unit, note: note || null },
                  })
                  setEditingItemId(null)
                }}
                onCancelEdit={() => setEditingItemId(null)}
              />
            ))}
          </ul>
        </section>

        {doneItems.length > 0 && (
          <section aria-labelledby="done-heading" className="done-section">
            <h2 id="done-heading" className="section-heading">
              Done ({doneItems.length})
            </h2>
            <ul className="row-list">
              {doneItems.map((item) => (
                <ItemRow
                  key={item.id}
                  item={item}
                  meId={me?.id}
                  unsynced={isUnsynced(item.id)}
                  onToggle={handleToggle}
                  onEdit={(i) => setEditingItemId(i.id)}
                  onRemove={handleRemoveItem}
                  editing={editingItemId === item.id}
                  onSaveEdit={(name, quantity, unit, note) => {
                    updateItem.mutate({
                      itemId: item.id,
                      body: { name, quantity, unit, note: note || null },
                    })
                    setEditingItemId(null)
                  }}
                  onCancelEdit={() => setEditingItemId(null)}
                />
              ))}
            </ul>
          </section>
        )}
      </div>

      <form className="quick-add" onSubmit={handleAddItem}>
        <label htmlFor="new-item-name">Add item</label>
        <div className="quick-add-row">
          <input
            id="new-item-name"
            ref={itemInputRef}
            type="text"
            value={itemName}
            onChange={(e) => setItemName(e.target.value)}
            placeholder="e.g. Milk"
            autoComplete="off"
          />
          <button type="submit" className="primary-button">
            Add
          </button>
        </div>
        <div className="quick-add-controls">
          {/* Additive quantity/unit controls (US-L.9): they never take focus
              from the name input, so name-only chained adds are unaffected. */}
          <div className="stepper" role="group" aria-label="Quantity">
            <button
              type="button"
              className="stepper-button"
              aria-label="Decrease quantity"
              onClick={() => setItemQuantity((q) => Math.max(1, q - 1))}
              disabled={itemQuantity <= 1}
            >
              −
            </button>
            <span className="stepper-value" aria-live="polite" data-testid="quick-add-quantity">
              {itemQuantity}
            </span>
            <button
              type="button"
              className="stepper-button"
              aria-label="Increase quantity"
              onClick={() => setItemQuantity((q) => q + 1)}
            >
              +
            </button>
          </div>
          <label className="quick-add-unit-label" htmlFor="new-item-unit">
            Unit
          </label>
          <select
            id="new-item-unit"
            className="quick-add-unit"
            value={itemUnit}
            onChange={(e) => setItemUnit(e.target.value as Unit)}
          >
            {units.map((u) => (
              <option key={u} value={u}>
                {unitLabels[u]}
              </option>
            ))}
          </select>
        </div>
      </form>

      <Snackbar entries={pending} onUndo={undo} />
    </div>
  )
}
