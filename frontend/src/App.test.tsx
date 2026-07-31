import { render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from './App'
import { withQueryClient } from './testUtils'
import type { ProblemDetail, User } from './api'

function jsonResponse(body: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: 'OK',
    headers: new Headers({ 'Content-Type': 'application/json' }),
    json: () => Promise.resolve(body),
  } as Response
}

function problemResponse(problem: ProblemDetail): Response {
  return {
    ok: false,
    status: problem.status ?? 401,
    statusText: 'Unauthorized',
    headers: new Headers({ 'Content-Type': 'application/problem+json' }),
    json: () => Promise.resolve(problem),
  } as Response
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('App', () => {
  it('renders a heading even when /me fails with a non-auth error', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.reject(new Error('no network in tests'))),
    )

    render(<App />, { wrapper: withQueryClient() })

    expect(await screen.findByRole('heading', { level: 1 })).toBeInTheDocument()
  })

  it('shows a log-in button and prompt on a 401, and never fetches lists', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      if (String(input).includes('/api/v1/me')) {
        return Promise.resolve(
          problemResponse({ type: '/problems/unauthorized', title: 'Unauthorized', status: 401 }),
        )
      }
      throw new Error(`unexpected fetch: ${String(input)}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<App />, { wrapper: withQueryClient() })

    expect(await screen.findByRole('button', { name: 'Log in' })).toBeInTheDocument()
    expect(screen.getByText(/sign in/i)).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/lists'),
      expect.anything(),
    )
  })

  it('shows the user name and a log-out button when signed in, and renders the lists UI', async () => {
    const user: User = { id: 'u1', name: 'Dev User' }
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        if (String(input).includes('/api/v1/me')) {
          return Promise.resolve(jsonResponse(user))
        }
        if (String(input).includes('/api/v1/lists')) {
          return Promise.resolve(jsonResponse([]))
        }
        throw new Error(`unexpected fetch: ${String(input)}`)
      }),
    )

    render(<App />, { wrapper: withQueryClient() })

    expect(await screen.findByText('Dev User')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Log out' })).toBeInTheDocument()
    await waitFor(() =>
      expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('Your lists'),
    )
  })
})
