import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getAuthConfig, getMe, isUnauthorized, login, logout } from './api'
import { ListsOverview } from './ListsOverview'
import { ListDetail } from './ListDetail'
import { LoginForm } from './LoginForm'
import { OfflineIndicator } from './OfflineIndicator'
import { randomId, subscribeSyncNotice } from './queries'

const SYNC_NOTICE_MS = 6000

interface SyncNotice {
  id: string
  message: string
}

/**
 * Quiet, auto-dismissing notices for offline changes the outbox had to drop
 * (US-O.3 replay-404): a queued mutation that hits a 404 on replay is not
 * re-queued — the affected keys refetch and this surfaces a passive toast.
 * Not a blocking modal, and not undoable — a Dismiss action only.
 */
function useSyncNotices() {
  const [notices, setNotices] = useState<SyncNotice[]>([])
  useEffect(() => {
    // Track the auto-dismiss timers so they can be cleared on unmount (a late
    // setNotices after unmount would otherwise warn / leak).
    const timers = new Set<ReturnType<typeof setTimeout>>()
    // TODO(US-O.3): debounce/aggregate a flood of notices when many mutations
    // fail to sync at once, rather than stacking one auto-dismissing toast each.
    const unsubscribe = subscribeSyncNotice((message) => {
      // randomId() (not bare crypto.randomUUID) so this can't throw in a
      // non-secure context — it fires inside the mutation onError path.
      const id = randomId()
      setNotices((current) => [...current, { id, message }])
      const timer = setTimeout(() => {
        timers.delete(timer)
        setNotices((current) => current.filter((n) => n.id !== id))
      }, SYNC_NOTICE_MS)
      timers.add(timer)
    })
    return () => {
      unsubscribe()
      timers.forEach(clearTimeout)
    }
  }, [])
  const dismiss = (id: string) => setNotices((current) => current.filter((n) => n.id !== id))
  return { notices, dismiss }
}

function SyncNotices({
  notices,
  onDismiss,
}: {
  notices: SyncNotice[]
  onDismiss: (id: string) => void
}) {
  if (notices.length === 0) return null
  return (
    <div className="snackbar-stack" role="status" aria-live="polite">
      {notices.map((notice) => (
        <div className="snackbar" key={notice.id}>
          <span>{notice.message}</span>
          <button type="button" className="snackbar-undo" onClick={() => onDismiss(notice.id)}>
            Dismiss
          </button>
        </div>
      ))}
    </div>
  )
}

type View = { screen: 'overview' } | { screen: 'list'; listId: string }

const meKey = ['me'] as const
const authConfigKey = ['authConfig'] as const

/**
 * Auth gate (US-A.2/A.4): resolves `GET /me` once and renders one of three
 * states — quiet loading (no blocking spinner), signed-in (account bar + the
 * existing lists UI), or signed-out (login prompt, no lists fetch). In dev
 * mode `getMe` always succeeds, so the signed-out state never appears; in
 * OIDC mode a 401 drives it.
 */
function App() {
  const [view, setView] = useState<View>({ screen: 'overview' })
  const { notices, dismiss } = useSyncNotices()
  const {
    data: user,
    error,
    isPending,
  } = useQuery({
    queryKey: meKey,
    queryFn: getMe,
    retry: false,
  })
  // The auth mode decides the signed-out UI (password form vs OIDC redirect).
  // It's public and rarely changes, so cache it and never retry; an
  // unresolved/failed lookup falls back to the OIDC redirect button below.
  const { data: authConfig } = useQuery({
    queryKey: authConfigKey,
    queryFn: getAuthConfig,
    retry: false,
    staleTime: Infinity,
  })

  if (user) {
    return (
      <>
        <OfflineIndicator />
        <div className="screen-header account-bar">
          <span className="account-bar-name">{user.name}</span>
          <button type="button" className="secondary-button" onClick={() => logout()}>
            Log out
          </button>
        </div>
        {view.screen === 'overview' ? (
          <ListsOverview onOpenList={(listId) => setView({ screen: 'list', listId })} />
        ) : (
          <ListDetail
            listId={view.listId}
            onBack={() => setView({ screen: 'overview' })}
            onDeleted={() => setView({ screen: 'overview' })}
          />
        )}
        <SyncNotices notices={notices} onDismiss={dismiss} />
      </>
    )
  }

  const unauthorized = !isPending && error !== null && isUnauthorized(error)

  return (
    <div className="screen">
      <header className="screen-header">
        <h1>Splitkauf</h1>
      </header>
      <div className="scroll-area">
        {isPending && (
          <p className="hint" aria-live="polite">
            Loading…
          </p>
        )}
        {unauthorized &&
          (authConfig?.mode === 'password' ? (
            <>
              <p className="hint">Sign in to see your shopping lists.</p>
              <LoginForm />
            </>
          ) : (
            <>
              <p className="hint">Sign in to see your shopping lists.</p>
              <button type="button" className="primary-button" onClick={() => login()}>
                Log in
              </button>
            </>
          ))}
        {!isPending && error !== null && !unauthorized && (
          <p className="hint hint-error">Couldn't load your account.</p>
        )}
      </div>
    </div>
  )
}

export default App
