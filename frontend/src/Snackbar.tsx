interface SnackbarEntry {
  id: string
  message: string
}

interface SnackbarProps {
  entries: SnackbarEntry[]
  onUndo: (id: string) => void
}

/** Stack of "Deleted — Undo" toasts (US-L.6/L.3). Never a confirm() dialog. */
export function Snackbar({ entries, onUndo }: SnackbarProps) {
  if (entries.length === 0) return null

  return (
    <div className="snackbar-stack" role="status" aria-live="polite">
      {entries.map((entry) => (
        <div className="snackbar" key={entry.id}>
          <span>{entry.message}</span>
          <button type="button" className="snackbar-undo" onClick={() => onUndo(entry.id)}>
            Undo
          </button>
        </div>
      ))}
    </div>
  )
}
