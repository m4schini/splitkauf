import { useEffect, useState } from 'react'
import { onlineManager } from '@tanstack/react-query'

/**
 * Quiet offline banner (US-O.2). Tracks React Query's `onlineManager` (the
 * same online/offline signal that gates paused mutations) rather than
 * `navigator.onLine` directly, so the indicator and the actual offline
 * behavior never disagree.
 *
 * Deliberately NOT a blocking spinner or modal — per
 * `docs/agents/research/2026-07-31-mobile-first-shopping-list-ux.md` §6 this
 * is a passive, non-interactive status line with no dismiss action and no
 * effect on layout beyond its own row.
 */
export function OfflineIndicator() {
  const [isOnline, setIsOnline] = useState(() => onlineManager.isOnline())

  useEffect(() => onlineManager.subscribe(() => setIsOnline(onlineManager.isOnline())), [])

  if (isOnline) return null

  return (
    <p className="offline-indicator" role="status" aria-live="polite">
      Offline — changes sync when you're back online
    </p>
  )
}
