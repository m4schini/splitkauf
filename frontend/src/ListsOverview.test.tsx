import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ListsOverview } from './ListsOverview'
import { createTestQueryClient, withQueryClient } from './testUtils'
import { listsKey } from './queries'
import type { List } from './api'

function jsonResponse(body: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: 'OK',
    headers: new Headers({ 'Content-Type': 'application/json' }),
    json: () => Promise.resolve(body),
  } as Response
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('ListsOverview', () => {
  it('renders lists with open/checked counts', async () => {
    const lists: List[] = [
      {
        id: 'l1',
        name: 'Groceries',
        openItemCount: 3,
        checkedItemCount: 1,
        createdAt: '2026-01-01T00:00:00Z',
        updatedAt: '2026-01-01T00:00:00Z',
      },
    ]
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(jsonResponse(lists))),
    )

    render(<ListsOverview onOpenList={() => {}} />, { wrapper: withQueryClient() })

    expect(await screen.findByText('Groceries')).toBeInTheDocument()
    expect(screen.getByText('3 open · 1 done')).toBeInTheDocument()
  })

  it('optimistically adds a new list and keeps the input focused', async () => {
    const user = userEvent.setup()
    vi.stubGlobal(
      'fetch',
      vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
        if (init?.method === 'POST') {
          const body = JSON.parse(init.body as string) as { name: string }
          return Promise.resolve(
            jsonResponse(
              {
                id: 'new-1',
                name: body.name,
                openItemCount: 0,
                checkedItemCount: 0,
                createdAt: '2026-01-01T00:00:00Z',
                updatedAt: '2026-01-01T00:00:00Z',
              },
              201,
            ),
          )
        }
        return Promise.resolve(jsonResponse([]))
      }),
    )

    render(<ListsOverview onOpenList={() => {}} />, { wrapper: withQueryClient() })

    const input = await screen.findByLabelText('New list name')
    await user.type(input, 'Weekend BBQ')
    await user.click(screen.getByRole('button', { name: 'Add' }))

    expect(await screen.findByText('Weekend BBQ')).toBeInTheDocument()
    expect(input).toHaveFocus()
    expect(input).toHaveValue('')
  })

  it('removes a list immediately on delete and shows an undo snackbar', async () => {
    const user = userEvent.setup()
    const lists: List[] = [
      {
        id: 'l1',
        name: 'Groceries',
        openItemCount: 0,
        checkedItemCount: 0,
        createdAt: '2026-01-01T00:00:00Z',
        updatedAt: '2026-01-01T00:00:00Z',
      },
    ]
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(jsonResponse(lists))),
    )

    render(<ListsOverview onOpenList={() => {}} />, { wrapper: withQueryClient() })
    await screen.findByText('Groceries')

    await user.click(screen.getByRole('button', { name: 'Delete list Groceries' }))

    await waitFor(() => expect(screen.queryByText('Groceries')).not.toBeInTheDocument())
    expect(screen.getByText('Deleted "Groceries" — Undo')).toBeInTheDocument()
  })

  it('undoing one of two overlapping deletes restores only that list, leaving the other removed from the cache', async () => {
    const user = userEvent.setup()
    const lists: List[] = [
      {
        id: 'l1',
        name: 'Groceries',
        openItemCount: 0,
        checkedItemCount: 0,
        createdAt: '2026-01-01T00:00:00Z',
        updatedAt: '2026-01-01T00:00:00Z',
      },
      {
        id: 'l2',
        name: 'Hardware',
        openItemCount: 0,
        checkedItemCount: 0,
        createdAt: '2026-01-01T00:00:00Z',
        updatedAt: '2026-01-01T00:00:00Z',
      },
    ]
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(jsonResponse(lists))),
    )

    const client = createTestQueryClient()
    render(<ListsOverview onOpenList={() => {}} />, { wrapper: withQueryClient(client) })
    await screen.findByText('Groceries')
    await screen.findByText('Hardware')

    // Delete A, then delete B (overlapping undo windows).
    await user.click(screen.getByRole('button', { name: 'Delete list Groceries' }))
    await user.click(screen.getByRole('button', { name: 'Delete list Hardware' }))

    // Both should already be gone from the underlying cache, not just hidden
    // by the pending-undo UI filter.
    expect(client.getQueryData<List[]>(listsKey)).toEqual([])

    // Undo A only. Scope to its own snackbar entry since both toasts render
    // an "Undo" button.
    const groceriesToast = screen.getByText('Deleted "Groceries" — Undo').closest('.snackbar')
    if (!groceriesToast) throw new Error('expected a snackbar entry for Groceries')
    await user.click(within(groceriesToast as HTMLElement).getByRole('button', { name: 'Undo' }))

    // The cache must now contain exactly Groceries — a full-snapshot restore
    // would have resurrected Hardware too.
    expect(client.getQueryData<List[]>(listsKey)).toEqual([lists[0]])
    expect(screen.getByText('Groceries')).toBeInTheDocument()
    expect(screen.queryByText('Hardware')).not.toBeInTheDocument()
  })
})
