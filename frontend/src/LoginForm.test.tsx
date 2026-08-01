import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { LoginForm } from './LoginForm'
import { createTestQueryClient, withQueryClient } from './testUtils'
import type { ProblemDetail } from './api'

function noContentResponse(): Response {
  return {
    ok: true,
    status: 204,
    statusText: 'No Content',
    headers: new Headers(),
    json: () => Promise.reject(new Error('should not be called for 204')),
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

describe('LoginForm', () => {
  it('POSTs the credentials and invalidates the me query on success', async () => {
    const fetchMock = vi.fn((_input: RequestInfo | URL, _init?: RequestInit) =>
      Promise.resolve(noContentResponse()),
    )
    vi.stubGlobal('fetch', fetchMock)
    const client = createTestQueryClient()
    const invalidate = vi.spyOn(client, 'invalidateQueries')

    render(<LoginForm />, { wrapper: withQueryClient(client) })

    await userEvent.type(screen.getByLabelText('Username'), 'alex')
    await userEvent.type(screen.getByLabelText('Password'), 'correct horse')
    await userEvent.click(screen.getByRole('button', { name: 'Log in' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
    const call = fetchMock.mock.calls[0]
    expect(String(call[0])).toBe('/api/auth/login')
    const init = call[1] as RequestInit
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body as string)).toEqual({
      username: 'alex',
      password: 'correct horse',
    })
    await waitFor(() => expect(invalidate).toHaveBeenCalledWith({ queryKey: ['me'] }))
  })

  it('shows an indistinguishable error on a 401 and does not invalidate me', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          problemResponse({ type: '/problems/unauthorized', title: 'Unauthorized', status: 401 }),
        ),
      ),
    )
    const client = createTestQueryClient()
    const invalidate = vi.spyOn(client, 'invalidateQueries')

    render(<LoginForm />, { wrapper: withQueryClient(client) })

    await userEvent.type(screen.getByLabelText('Username'), 'alex')
    await userEvent.type(screen.getByLabelText('Password'), 'wrong')
    await userEvent.click(screen.getByRole('button', { name: 'Log in' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Invalid username or password.')
    expect(invalidate).not.toHaveBeenCalled()
  })
})
