import { useCallback, useEffect, useRef, useState } from 'react'

const UNDO_WINDOW_MS = 5000

interface PendingUndo {
  id: string
  message: string
}

interface Entry {
  timeoutId: ReturnType<typeof setTimeout>
  commit: () => void
}

/**
 * Generic "delete with undo" queue (US-L.6 / US-L.3): the caller applies its
 * own optimistic removal immediately, then calls `schedule` with a `commit`
 * callback that performs the real deletion. If the user doesn't click Undo
 * within the window, `commit` fires; if they do, `restore` runs instead and
 * `commit` never happens. No confirm() dialogs anywhere in this flow.
 *
 * If the owning component unmounts while a delete is still pending (e.g. the
 * user navigates away), the pending deletion is finalized immediately rather
 * than silently dropped, so the item is never left removed from the UI but
 * still present on the server.
 */
export function useUndoQueue() {
  const [pending, setPending] = useState<PendingUndo[]>([])
  const entries = useRef(new Map<string, Entry>())
  const restores = useRef(new Map<string, () => void>())

  useEffect(() => {
    const map = entries.current
    return () => {
      map.forEach((entry) => {
        clearTimeout(entry.timeoutId)
        entry.commit()
      })
      map.clear()
    }
  }, [])

  const schedule = useCallback(
    (id: string, message: string, commit: () => void, restore: () => void) => {
      restores.current.set(id, restore)
      setPending((old) => [...old, { id, message }])
      const timeoutId = setTimeout(() => {
        entries.current.delete(id)
        restores.current.delete(id)
        setPending((old) => old.filter((p) => p.id !== id))
        commit()
      }, UNDO_WINDOW_MS)
      entries.current.set(id, { timeoutId, commit })
    },
    [],
  )

  const undo = useCallback((id: string) => {
    const entry = entries.current.get(id)
    if (entry) clearTimeout(entry.timeoutId)
    entries.current.delete(id)
    const restore = restores.current.get(id)
    restores.current.delete(id)
    setPending((old) => old.filter((p) => p.id !== id))
    restore?.()
  }, [])

  return { pending, schedule, undo }
}
