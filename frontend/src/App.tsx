import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getMe, isUnauthorized, login, logout } from './api'
import { ListsOverview } from './ListsOverview'
import { ListDetail } from './ListDetail'

type View = { screen: 'overview' } | { screen: 'list'; listId: string }

const meKey = ['me'] as const

/**
 * Auth gate (US-A.2/A.4): resolves `GET /me` once and renders one of three
 * states — quiet loading (no blocking spinner), signed-in (account bar + the
 * existing lists UI), or signed-out (login prompt, no lists fetch). In dev
 * mode `getMe` always succeeds, so the signed-out state never appears; in
 * OIDC mode a 401 drives it.
 */
function App() {
  const [view, setView] = useState<View>({ screen: 'overview' })
  const {
    data: user,
    error,
    isPending,
  } = useQuery({
    queryKey: meKey,
    queryFn: getMe,
    retry: false,
  })

  if (user) {
    return (
      <>
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
        {unauthorized && (
          <>
            <p className="hint">Sign in to see your shopping lists.</p>
            <button type="button" className="primary-button" onClick={() => login()}>
              Log in
            </button>
          </>
        )}
        {!isPending && error !== null && !unauthorized && (
          <p className="hint hint-error">Couldn't load your account.</p>
        )}
      </div>
    </div>
  )
}

export default App
