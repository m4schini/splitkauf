import { useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { deleteList as apiDeleteList, type List } from './api'
import { Snackbar } from './Snackbar'
import { useUndoQueue } from './useUndoQueue'
import { listsKey, removeListLocally, restoreListLocally, useCreateList, useLists } from './queries'
import { useLiveEvents } from './live'

interface ListsOverviewProps {
  onOpenList: (listId: string) => void
}

/** Overview of all shopping lists (US-L.1/L.2), with a bottom-anchored create form. */
export function ListsOverview({ onOpenList }: ListsOverviewProps) {
  const { data: lists, isLoading, isError } = useLists()
  const createList = useCreateList()
  const queryClient = useQueryClient()
  const { pending, schedule, undo } = useUndoQueue()
  const [name, setName] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)

  // Live updates (M3/US-S.1): any list or item mutation elsewhere can change
  // what belongs in this overview (names, open/checked counts), and a
  // reconnect may have missed hints while disconnected — so all three event
  // kinds trigger a quiet refetch (no blocking spinner; React Query just
  // refreshes the cache in place).
  useLiveEvents(() => {
    queryClient.invalidateQueries({ queryKey: listsKey })
  })

  function handleCreate(e: React.FormEvent) {
    e.preventDefault()
    const trimmed = name.trim()
    if (!trimmed) return
    createList.mutate(trimmed)
    setName('')
    inputRef.current?.focus()
  }

  async function handleDelete(list: List) {
    await queryClient.cancelQueries({ queryKey: listsKey })
    const index =
      queryClient.getQueryData<List[]>(listsKey)?.findIndex((l) => l.id === list.id) ?? -1
    removeListLocally(queryClient, list)
    schedule(
      list.id,
      `Deleted "${list.name}" — Undo`,
      () => {
        apiDeleteList(list.id).catch(() => {
          queryClient.invalidateQueries({ queryKey: listsKey })
        })
      },
      () => {
        restoreListLocally(queryClient, list, index)
      },
    )
  }

  const visibleLists = (lists ?? []).filter((l) => !pending.some((p) => p.id === l.id))

  return (
    <div className="screen">
      <header className="screen-header">
        <h1>Your lists</h1>
      </header>

      <div className="scroll-area">
        {isLoading && <p className="hint">Loading…</p>}
        {isError && <p className="hint hint-error">Couldn't load lists.</p>}
        {!isLoading && visibleLists.length === 0 && (
          <p className="hint">No lists yet — add one below.</p>
        )}
        <ul className="row-list">
          {visibleLists.map((list) => (
            <li key={list.id} className="row">
              <button
                type="button"
                className="row-tap-target list-row-button"
                onClick={() => onOpenList(list.id)}
              >
                <span className="list-row-name">{list.name}</span>
                <span className="list-row-counts">
                  {list.openItemCount} open
                  {list.checkedItemCount > 0 ? ` · ${list.checkedItemCount} done` : ''}
                </span>
              </button>
              <button
                type="button"
                className="icon-button danger"
                aria-label={`Delete list ${list.name}`}
                onClick={() => handleDelete(list)}
              >
                Delete
              </button>
            </li>
          ))}
        </ul>
      </div>

      <form className="quick-add" onSubmit={handleCreate}>
        <label htmlFor="new-list-name">New list name</label>
        <div className="quick-add-row">
          <input
            id="new-list-name"
            ref={inputRef}
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="e.g. Weekly groceries"
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
