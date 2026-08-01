import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { isUnauthorized, passwordLogin } from './api'

/**
 * Username/password sign-in form (US-A.6), shown when the server reports
 * password auth mode. On success it invalidates the `['me']` query so the auth
 * gate re-resolves and flips to the signed-in view — no full-page reload. A
 * failed sign-in shows a single inline message that does not reveal whether the
 * username exists (matching the server's indistinguishable 401). The password
 * is submitted over the existing fetch path and never persisted client-side.
 */
export function LoginForm() {
  const queryClient = useQueryClient()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      await passwordLogin(username, password)
      // Re-resolve the auth gate; a successful login flips to the signed-in view.
      await queryClient.invalidateQueries({ queryKey: ['me'] })
    } catch (err) {
      setError(
        isUnauthorized(err)
          ? 'Invalid username or password.'
          : 'Could not sign in. Please try again.',
      )
      setSubmitting(false)
    }
  }

  return (
    <form className="login-form" onSubmit={handleSubmit}>
      <label htmlFor="login-username">Username</label>
      <input
        id="login-username"
        name="username"
        autoComplete="username"
        autoCapitalize="none"
        autoCorrect="off"
        spellCheck={false}
        value={username}
        onChange={(e) => setUsername(e.target.value)}
        required
      />
      <label htmlFor="login-password">Password</label>
      <input
        id="login-password"
        name="password"
        type="password"
        autoComplete="current-password"
        value={password}
        onChange={(e) => setPassword(e.target.value)}
        required
      />
      {error && (
        <p className="hint hint-error" role="alert" aria-live="assertive">
          {error}
        </p>
      )}
      <button type="submit" className="primary-button" disabled={submitting}>
        {submitting ? 'Signing in…' : 'Log in'}
      </button>
    </form>
  )
}
