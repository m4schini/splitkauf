import type { PropsWithChildren } from 'react'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  dehydrate,
  hydrate,
  QueryClient,
  QueryClientProvider,
  onlineManager,
} from '@tanstack/react-query'
import { ListDetail } from './ListDetail'
import { is4xxProblem, randomId, registerMutationDefaults, subscribeSyncNotice } from './queries'
import type { ListWithItems, ProblemDetail } from './api'

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
    status: problem.status ?? 500,
    statusText: 'Error',
    headers: new Headers({ 'Content-Type': 'application/problem+json' }),
    json: () => Promise.resolve(problem),
  } as Response
}

const baseList: ListWithItems = {
  id: 'l1',
  name: 'Groceries',
  openItemCount: 1,
  checkedItemCount: 0,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
  items: [
    {
      id: 'i1',
      listId: 'l1',
      name: 'Milk',
      quantity: 1,
      note: null,
      checked: false,
      checkedAt: null,
      createdAt: '2026-01-01T00:00:00Z',
      updatedAt: '2026-01-01T00:00:00Z',
    },
  ],
}

/** A client that won't refetch on reconnect (which would clobber optimistic
 *  offline state) and inherits the real per-mutation retry/networkMode. Mutation
 *  defaults are registered so a rehydrated (post-reload) client can resume its
 *  persisted paused mutations with no hook mounted. */
function offlineClient(): QueryClient {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchOnReconnect: false, refetchOnWindowFocus: false },
      mutations: { retry: false },
    },
  })
  registerMutationDefaults(client)
  return client
}

/**
 * Simulates a browser reload: dehydrates the persisted state (paused mutations
 * + cached queries) from `from`, JSON round-trips it (as the IndexedDB
 * persister does), and hydrates it onto a fresh client with no live hooks.
 */
function reload(from: QueryClient): QueryClient {
  const dehydrated = JSON.parse(JSON.stringify(dehydrate(from)))
  const to = offlineClient()
  hydrate(to, dehydrated)
  return to
}

function wrapper(client: QueryClient) {
  return function Wrapper({ children }: PropsWithChildren) {
    return <QueryClientProvider client={client}>{children}</QueryClientProvider>
  }
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  onlineManager.setOnline(true)
})

describe('offline outbox — pending-create coalescing (US-O.2 KD3)', () => {
  it('collapses an offline add → check into a single POST carrying the merged state', async () => {
    const user = userEvent.setup()
    let online = true
    const posted: Array<{ name: string; checked?: boolean }> = []
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (init?.method === 'POST' && url.endsWith('/items')) {
        if (!online) return Promise.reject(new TypeError('offline'))
        const body = JSON.parse(init.body as string) as { name: string; checked?: boolean }
        posted.push(body)
        return Promise.resolve(
          jsonResponse(
            {
              id: 'srv-eggs',
              listId: 'l1',
              name: body.name,
              quantity: 1,
              note: null,
              checked: body.checked ?? false,
              checkedAt: body.checked ? '2026-01-02T00:00:00Z' : null,
              createdAt: '2026-01-02T00:00:00Z',
              updatedAt: '2026-01-02T00:00:00Z',
            },
            201,
          ),
        )
      }
      // Any /check, /uncheck, PATCH here would mean coalescing failed.
      if (url.includes('/check') || url.includes('/uncheck') || init?.method === 'PATCH') {
        throw new Error(`unexpected coalesced mutation reached the network: ${init?.method} ${url}`)
      }
      return Promise.resolve(jsonResponse(baseList))
    })
    vi.stubGlobal('fetch', fetchMock)

    const client = offlineClient()
    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} />, {
      wrapper: wrapper(client),
    })
    await screen.findByText('Milk')

    // Go offline: the add's first attempt fails and the mutation pauses.
    online = false
    onlineManager.setOnline(false)

    const input = screen.getByLabelText('Add item')
    await user.type(input, 'Eggs')
    await user.click(screen.getByRole('button', { name: 'Add' }))

    // The offline item shows the unsynced badge and no POST has landed.
    expect(await screen.findByText('Eggs')).toBeInTheDocument()
    expect(await screen.findByText('Unsynced')).toBeInTheDocument()
    expect(posted).toHaveLength(0)

    // Check the still-queued item: folds into the create, enqueues nothing.
    await user.click(screen.getByRole('checkbox', { name: /Eggs/ }))
    await waitFor(() => expect(screen.getByText('Done (1)')).toBeInTheDocument())

    // Reconnect and replay the outbox: exactly one POST, carrying checked=true.
    online = true
    onlineManager.setOnline(true)
    await client.resumePausedMutations()

    await waitFor(() => expect(posted).toHaveLength(1))
    expect(posted[0]).toMatchObject({ name: 'Eggs', checked: true })

    // The temp item is replaced by the server item: the badge clears and the
    // item stays checked in the Done section.
    await waitFor(() => expect(screen.queryByText('Unsynced')).not.toBeInTheDocument())
    const done = screen.getByRole('heading', { name: /Done/ }).closest('section') as HTMLElement
    expect(within(done).getByText('Eggs')).toBeInTheDocument()
  })

  it('carries a folded payload across a reload: resume POSTs the merged state', async () => {
    // Regression for the "fold lost on reload" bug: the fold lives in the
    // paused mutation's variables (dehydrated by the persister), NOT an
    // in-memory map, so a POST replayed after a reload still carries checked=true.
    const user = userEvent.setup()
    let online = true
    const posted: Array<{ name: string; checked?: boolean }> = []
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (init?.method === 'POST' && url.endsWith('/items')) {
        if (!online) return Promise.reject(new TypeError('offline'))
        const body = JSON.parse(init.body as string) as { name: string; checked?: boolean }
        posted.push(body)
        return Promise.resolve(
          jsonResponse({ ...baseList.items[0], id: 'srv-eggs', name: body.name }, 201),
        )
      }
      return Promise.resolve(jsonResponse(baseList))
    })
    vi.stubGlobal('fetch', fetchMock)

    const client = offlineClient()
    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} />, {
      wrapper: wrapper(client),
    })
    await screen.findByText('Milk')

    online = false
    onlineManager.setOnline(false)
    await user.type(screen.getByLabelText('Add item'), 'Eggs')
    await user.click(screen.getByRole('button', { name: 'Add' }))
    expect(await screen.findByText('Unsynced')).toBeInTheDocument()

    // Fold a check into the still-paused create.
    await user.click(screen.getByRole('checkbox', { name: /Eggs/ }))
    await waitFor(() => expect(screen.getByText('Done (1)')).toBeInTheDocument())

    // Reload: dehydrate → JSON round-trip → hydrate onto a fresh client with no
    // hooks mounted, then reconnect and resume the persisted outbox.
    const reloaded = reload(client)
    online = true
    onlineManager.setOnline(true)
    await reloaded.resumePausedMutations()

    await waitFor(() => expect(posted).toHaveLength(1))
    expect(posted[0]).toMatchObject({ name: 'Eggs', checked: true })
  })

  it('falls through to a real mutation when a temp check has no paused create (in-flight)', async () => {
    // The create is in-flight online (not paused), so a check on its temp id
    // cannot fold — it MUST enqueue a real check, not be silently dropped.
    const user = userEvent.setup()
    let resolvePost!: (value: Response) => void
    const checks: string[] = []
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (init?.method === 'POST' && url.endsWith('/items')) {
        return new Promise<Response>((resolve) => {
          resolvePost = resolve
        })
      }
      if (url.includes('/check')) {
        checks.push(url)
        return Promise.resolve(jsonResponse(baseList))
      }
      return Promise.resolve(jsonResponse(baseList))
    })
    vi.stubGlobal('fetch', fetchMock)

    const client = offlineClient()
    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} />, {
      wrapper: wrapper(client),
    })
    await screen.findByText('Milk')

    // Add online: the POST is left in-flight (unresolved), so the create is
    // NOT paused while we check the optimistic temp item.
    await user.type(screen.getByLabelText('Add item'), 'Eggs')
    await user.click(screen.getByRole('button', { name: 'Add' }))
    await screen.findByText('Eggs')

    await user.click(screen.getByRole('checkbox', { name: /Eggs/ }))
    // A real check mutation reached the network — the change was not dropped.
    await waitFor(() => expect(checks).toHaveLength(1))

    // Let the create settle so no unhandled promise lingers.
    resolvePost(jsonResponse({ ...baseList.items[0], id: 'srv-eggs', name: 'Eggs' }, 201))
    await waitFor(() => expect(client.getMutationCache().getAll().length).toBeGreaterThan(0))
  })

  it('cancels the queued create when the offline item is deleted (no POST)', async () => {
    const user = userEvent.setup()
    let online = true
    const posted: unknown[] = []
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (init?.method === 'POST' && url.endsWith('/items')) {
        if (!online) return Promise.reject(new TypeError('offline'))
        posted.push(JSON.parse(init.body as string))
        return Promise.resolve(jsonResponse({ ...baseList.items[0], id: 'srv', name: 'Eggs' }, 201))
      }
      if (init?.method === 'DELETE') {
        throw new Error('a queued create must never issue a DELETE for its temp id')
      }
      return Promise.resolve(jsonResponse(baseList))
    })
    vi.stubGlobal('fetch', fetchMock)

    const client = offlineClient()
    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} />, {
      wrapper: wrapper(client),
    })
    await screen.findByText('Milk')

    online = false
    onlineManager.setOnline(false)
    await user.type(screen.getByLabelText('Add item'), 'Eggs')
    await user.click(screen.getByRole('button', { name: 'Add' }))
    expect(await screen.findByText('Unsynced')).toBeInTheDocument()

    // Delete the offline item: cancels the paused create entirely.
    await user.click(screen.getByRole('button', { name: 'Remove Eggs' }))
    await waitFor(() => expect(screen.queryByText('Eggs')).not.toBeInTheDocument())

    // Reconnecting replays nothing — the create was removed from the outbox.
    online = true
    onlineManager.setOnline(true)
    await client.resumePausedMutations()

    expect(posted).toHaveLength(0)
    expect(client.getMutationCache().getAll()).toHaveLength(0)
  })
})

describe('offline outbox — badge & replay-404 (US-O.2/O.3)', () => {
  it('shows no unsynced badge for a normally-loaded (synced) item', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(jsonResponse(baseList))),
    )
    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} />, {
      wrapper: wrapper(offlineClient()),
    })
    await screen.findByText('Milk')
    expect(screen.queryByText('Unsynced')).not.toBeInTheDocument()
  })

  it('drops a replay that hits 404 and raises a quiet sync notice', async () => {
    const user = userEvent.setup()
    const notice = vi.fn()
    const unsubscribe = subscribeSyncNotice(notice)
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      if (String(input).endsWith('/check')) {
        return Promise.resolve(
          problemResponse({ status: 404, type: '/problems/not-found', title: 'Not Found' }),
        )
      }
      return Promise.resolve(jsonResponse(baseList))
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} />, {
      wrapper: wrapper(offlineClient()),
    })
    await screen.findByText('Milk')
    await user.click(screen.getByRole('checkbox', { name: /Milk/ }))

    await waitFor(() => expect(notice).toHaveBeenCalledWith("Some offline changes couldn't sync"))
    // The change is dropped (not re-queued): the refetch leaves Milk open.
    await waitFor(() => expect(screen.getByText('Open (1)')).toBeInTheDocument())
    unsubscribe()
  })

  it('drops a replay that hits a non-404 4xx (e.g. 409) with a quiet notice', async () => {
    const user = userEvent.setup()
    const notice = vi.fn()
    const unsubscribe = subscribeSyncNotice(notice)
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      if (String(input).endsWith('/check')) {
        return Promise.resolve(
          problemResponse({ status: 409, type: '/problems/conflict', title: 'Conflict' }),
        )
      }
      return Promise.resolve(jsonResponse(baseList))
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} />, {
      wrapper: wrapper(offlineClient()),
    })
    await screen.findByText('Milk')
    await user.click(screen.getByRole('checkbox', { name: /Milk/ }))

    await waitFor(() => expect(notice).toHaveBeenCalledWith("Some offline changes couldn't sync"))
    unsubscribe()
  })

  it('keeps the unsynced badge after a reload (temp id persists)', async () => {
    const user = userEvent.setup()
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === 'POST' && String(input).endsWith('/items')) {
        return Promise.reject(new TypeError('offline'))
      }
      return Promise.resolve(jsonResponse(baseList))
    })
    vi.stubGlobal('fetch', fetchMock)

    const client = offlineClient()
    const { unmount } = render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} />, {
      wrapper: wrapper(client),
    })
    await screen.findByText('Milk')

    onlineManager.setOnline(false)
    await user.type(screen.getByLabelText('Add item'), 'Eggs')
    await user.click(screen.getByRole('button', { name: 'Add' }))
    expect(await screen.findByText('Unsynced')).toBeInTheDocument()

    // Reload: the persisted temp item keeps its temp id, so the badge survives.
    unmount()
    const reloaded = reload(client)
    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} />, {
      wrapper: wrapper(reloaded),
    })
    expect(await screen.findByText('Eggs')).toBeInTheDocument()
    expect(screen.getByText('Unsynced')).toBeInTheDocument()
  })
})

describe('randomId', () => {
  it('always yields a temp- prefix', () => {
    expect(randomId()).toMatch(/^temp-/)
  })

  it('falls back to a non-throwing id when crypto.randomUUID is undefined', () => {
    vi.stubGlobal('crypto', { randomUUID: undefined })
    const id = randomId()
    expect(id).toMatch(/^temp-/)
    expect(id.length).toBeGreaterThan('temp-'.length)
  })
})

describe('is4xxProblem', () => {
  it('classifies a numeric 4xx status as fail-fast', () => {
    expect(is4xxProblem({ status: 404, type: '/problems/not-found', title: 'x' })).toBe(true)
    expect(is4xxProblem({ status: 409, type: '/problems/conflict', title: 'x' })).toBe(true)
  })

  it('treats a 5xx or a status-less problem as not-4xx (pause/retry)', () => {
    expect(is4xxProblem({ status: 500, type: '/problems/internal', title: 'x' })).toBe(false)
    // A status-less /problems/... must NOT fail fast solely on its type.
    expect(is4xxProblem({ type: '/problems/internal', title: 'x' })).toBe(false)
    expect(is4xxProblem(new TypeError('offline'))).toBe(false)
    expect(is4xxProblem(null)).toBe(false)
  })
})
