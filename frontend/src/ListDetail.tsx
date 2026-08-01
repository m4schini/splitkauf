import { useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { type Item, type List } from './api'
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
  useDeleteItemMutation,
  useDeleteListMutation,
  useListDetail,
  useRenameList,
  useRestoreItem,
  useUncheckItem,
  useUpdateItem,
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

interface ListDetailProps {
  listId: string
  onBack: () => void
  onDeleted: () => void
}

interface ItemRowProps {
  item: Item
  unsynced: boolean
  onToggle: (item: Item) => void
  onEdit: (item: Item) => void
  onRemove: (item: Item) => void
  editing: boolean
  onSaveEdit: (name: string, quantity: number, note: string) => void
  onCancelEdit: () => void
}

function ItemRow({
  item,
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
  const [note, setNote] = useState(item.note ?? '')

  if (editing) {
    return (
      <li className="row item-row item-row-editing">
        <form
          className="item-edit-form"
          onSubmit={(e) => {
            e.preventDefault()
            onSaveEdit(name, quantity, note)
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
          {item.quantity > 1 && <span className="item-qty"> ×{item.quantity}</span>}
          {item.note && <span className="item-note">{item.note}</span>}
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
          Edit
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
          Remove
        </button>
      </div>
    </li>
  )
}

/** List detail: open/done item sections, bottom quick-add, rename & delete list. */
export function ListDetail({ listId, onBack, onDeleted }: ListDetailProps) {
  const { data: list, isLoading, isError } = useListDetail(listId)
  const addItem = useAddItem(listId)
  const updateItem = useUpdateItem(listId)
  const checkItem = useCheckItem(listId)
  const uncheckItem = useUncheckItem(listId)
  const renameList = useRenameList(listId)
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
  const itemInputRef = useRef<HTMLInputElement>(null)
  const [editingItemId, setEditingItemId] = useState<string | null>(null)
  const [renaming, setRenaming] = useState(false)
  const [nameDraft, setNameDraft] = useState('')

  function handleAddItem(e: React.FormEvent) {
    e.preventDefault()
    const trimmed = itemName.trim()
    if (!trimmed) return
    addItem.mutate({ name: trimmed })
    setItemName('')
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
          Back
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
            onClick={startRename}
            aria-label="Rename list"
          >
            Rename
          </button>
          <button
            type="button"
            className="icon-button danger"
            onClick={handleDeleteList}
            aria-label="Delete list"
          >
            Delete
          </button>
        </div>
      </header>

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
                unsynced={isUnsynced(item.id)}
                onToggle={handleToggle}
                onEdit={(i) => setEditingItemId(i.id)}
                onRemove={handleRemoveItem}
                editing={editingItemId === item.id}
                onSaveEdit={(name, quantity, note) => {
                  updateItem.mutate({
                    itemId: item.id,
                    body: { name, quantity, note: note || null },
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
                  unsynced={isUnsynced(item.id)}
                  onToggle={handleToggle}
                  onEdit={(i) => setEditingItemId(i.id)}
                  onRemove={handleRemoveItem}
                  editing={editingItemId === item.id}
                  onSaveEdit={(name, quantity, note) => {
                    updateItem.mutate({
                      itemId: item.id,
                      body: { name, quantity, note: note || null },
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
      </form>

      <Snackbar entries={pending} onUndo={undo} />
    </div>
  )
}
