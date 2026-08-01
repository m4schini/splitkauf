import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ListDetail } from './ListDetail'
import { withQueryClient } from './testUtils'
import type { ListWithItems } from './api'

function jsonResponse(body: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: 'OK',
    headers: new Headers({ 'Content-Type': 'application/json' }),
    json: () => Promise.resolve(body),
  } as Response
}

function noContentResponse(): Response {
  return {
    ok: true,
    status: 204,
    statusText: 'No Content',
    headers: new Headers(),
    json: () => Promise.reject(new Error('should not be called for 204')),
  } as Response
}

afterEach(() => {
  vi.unstubAllGlobals()
})

const baseList: ListWithItems = {
  id: 'l1',
  name: 'Groceries',
  openItemCount: 1,
  checkedItemCount: 1,
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
    {
      id: 'i2',
      listId: 'l1',
      name: 'Bread',
      quantity: 1,
      note: null,
      checked: true,
      checkedAt: '2026-01-01T00:00:00Z',
      createdAt: '2026-01-01T00:00:00Z',
      updatedAt: '2026-01-01T00:00:00Z',
    },
  ],
}

describe('ListDetail', () => {
  it('splits items into open and done sections', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(jsonResponse(baseList))),
    )

    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} />, {
      wrapper: withQueryClient(),
    })

    expect(await screen.findByText('Open (1)')).toBeInTheDocument()
    expect(screen.getByText('Done (1)')).toBeInTheDocument()
    expect(screen.getByText('Milk')).toBeInTheDocument()
    expect(screen.getByText('Bread')).toBeInTheDocument()
  })

  it('adds an item optimistically and keeps the quick-add input focused', async () => {
    const user = userEvent.setup()
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        if (init?.method === 'POST' && String(input).endsWith('/items')) {
          const body = JSON.parse(init.body as string) as { name: string }
          return Promise.resolve(
            jsonResponse(
              {
                id: 'new-item',
                listId: 'l1',
                name: body.name,
                quantity: 1,
                note: null,
                checked: false,
                checkedAt: null,
                createdAt: '2026-01-01T00:00:00Z',
                updatedAt: '2026-01-01T00:00:00Z',
              },
              201,
            ),
          )
        }
        return Promise.resolve(jsonResponse(baseList))
      }),
    )

    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} />, {
      wrapper: withQueryClient(),
    })

    const input = await screen.findByLabelText('Add item')
    await user.type(input, 'Eggs')
    await user.click(screen.getByRole('button', { name: 'Add' }))

    expect(await screen.findByText('Eggs')).toBeInTheDocument()
    expect(input).toHaveFocus()
    expect(input).toHaveValue('')
  })

  it('checking an open item moves it into the done section', async () => {
    const user = userEvent.setup()
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        if (String(input).endsWith('/check')) {
          return Promise.resolve(
            jsonResponse({
              ...baseList.items[0],
              checked: true,
              checkedAt: '2026-01-01T00:00:00Z',
            }),
          )
        }
        return Promise.resolve(jsonResponse(baseList))
      }),
    )

    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} />, {
      wrapper: withQueryClient(),
    })

    await screen.findByText('Milk')
    const milkCheckbox = screen.getByRole('checkbox', { name: /Milk/ })
    await user.click(milkCheckbox)

    await waitFor(() => expect(screen.getByText('Done (2)')).toBeInTheDocument())
  })

  it('unchecking a done item moves it back into the open section', async () => {
    const user = userEvent.setup()
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        if (String(input).endsWith('/uncheck')) {
          return Promise.resolve(
            jsonResponse({
              ...baseList.items[1],
              checked: false,
              checkedAt: null,
            }),
          )
        }
        return Promise.resolve(jsonResponse(baseList))
      }),
    )

    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} />, {
      wrapper: withQueryClient(),
    })

    await screen.findByText('Bread')
    const breadCheckbox = screen.getByRole('checkbox', { name: /Bread/ })
    await user.click(breadCheckbox)

    await waitFor(() => expect(screen.getByText('Open (2)')).toBeInTheDocument())
    expect(screen.queryByText('Done (1)')).not.toBeInTheDocument()
  })

  it('removes an item immediately on remove (delete fires at once) and shows an undo snackbar', async () => {
    const user = userEvent.setup()
    const fetchMock = vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === 'DELETE') {
        return Promise.resolve(noContentResponse())
      }
      return Promise.resolve(jsonResponse(baseList))
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} />, {
      wrapper: withQueryClient(),
    })

    await screen.findByText('Milk')
    await user.click(screen.getByRole('button', { name: 'Remove Milk' }))

    await waitFor(() => expect(screen.queryByText('Milk')).not.toBeInTheDocument())
    expect(screen.getByText('Removed "Milk" — Undo')).toBeInTheDocument()
    // The delete fires immediately on removal, not after the undo window
    // elapses (item delete is a server-backed soft delete, US-O.2 KD4).
    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith('/api/v1/lists/l1/items/i1', { method: 'DELETE' }),
    )
  })

  it('undoing an item removal calls the restore endpoint (not a cancelled delete)', async () => {
    const user = userEvent.setup()
    // Mirrors the server's soft-delete/restore semantics: a background
    // refetch (triggered by the delete mutation's onSettled invalidation)
    // must not resurrect the item before the user clicks Undo.
    let deleted = false
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === 'DELETE') {
        deleted = true
        return Promise.resolve(noContentResponse())
      }
      if (init?.method === 'POST' && String(input).endsWith('/restore')) {
        deleted = false
        return Promise.resolve(jsonResponse(baseList.items[0]))
      }
      const items = deleted ? baseList.items.filter((i) => i.id !== 'i1') : baseList.items
      return Promise.resolve(jsonResponse({ ...baseList, items }))
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} />, {
      wrapper: withQueryClient(),
    })

    await screen.findByText('Milk')
    await user.click(screen.getByRole('button', { name: 'Remove Milk' }))
    await waitFor(() => expect(screen.queryByText('Milk')).not.toBeInTheDocument())

    await user.click(screen.getByRole('button', { name: 'Undo' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith('/api/v1/lists/l1/items/i1/restore', {
        method: 'POST',
      }),
    )
    expect(await screen.findByText('Milk')).toBeInTheDocument()
  })

  it('recovers the item when a delete-triggered refetch races an in-flight restore', async () => {
    const user = userEvent.setup()
    // Mirrors the real race a reviewer found: the DELETE and the restore POST
    // are both in flight at once, and the DELETE's onSettled-triggered GET
    // can resolve while the restore POST is still pending server-side,
    // clobbering the cache with a snapshot that's missing the item. The fix
    // is (a) an upsert in useRestoreItem's onSuccess and (b) an onSettled
    // invalidate on useRestoreItem itself, so the final state converges on
    // the item being present regardless of interleaving.
    let deleted = false
    let resolveDelete!: () => void
    let resolveRestore!: () => void
    const deletePromise = new Promise<void>((resolve) => {
      resolveDelete = resolve
    })
    const restorePromise = new Promise<void>((resolve) => {
      resolveRestore = resolve
    })

    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === 'DELETE') {
        return deletePromise.then(() => {
          deleted = true
          return noContentResponse()
        })
      }
      if (init?.method === 'POST' && String(input).endsWith('/restore')) {
        return restorePromise.then(() => {
          deleted = false
          return jsonResponse(baseList.items[0])
        })
      }
      // GET refetch (triggered by either mutation's onSettled invalidate):
      // reflects the soft-delete state at the moment it's called.
      const items = deleted ? baseList.items.filter((i) => i.id !== 'i1') : baseList.items
      return Promise.resolve(jsonResponse({ ...baseList, items }))
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} />, {
      wrapper: withQueryClient(),
    })

    await screen.findByText('Milk')
    await user.click(screen.getByRole('button', { name: 'Remove Milk' }))
    await waitFor(() => expect(screen.queryByText('Milk')).not.toBeInTheDocument())

    // Undo fires the restore POST while the DELETE is still unresolved.
    await user.click(screen.getByRole('button', { name: 'Undo' }))

    // Now let the DELETE resolve first: its onSettled invalidate refetches
    // the list, which (since the restore hasn't landed server-side yet)
    // comes back without the item.
    resolveDelete()
    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith('/api/v1/lists/l1/items/i1', { method: 'DELETE' }),
    )

    // Finally the restore POST resolves; the item must reappear regardless
    // of the delete's refetch having briefly dropped it from the cache.
    resolveRestore()
    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith('/api/v1/lists/l1/items/i1/restore', {
        method: 'POST',
      }),
    )
    expect(await screen.findByText('Milk')).toBeInTheDocument()
  })

  it('deleting the list shows an undo snackbar and eventually calls onDeleted', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const user = userEvent.setup({ delay: null })
    const onDeleted = vi.fn()
    vi.stubGlobal(
      'fetch',
      vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
        if (init?.method === 'DELETE') {
          return Promise.resolve(noContentResponse())
        }
        return Promise.resolve(jsonResponse(baseList))
      }),
    )

    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={onDeleted} />, {
      wrapper: withQueryClient(),
    })

    await screen.findByText('Milk')
    await user.click(screen.getByRole('button', { name: 'Delete list' }))

    expect(screen.getByText('List deleted.')).toBeInTheDocument()
    expect(onDeleted).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(5100)

    expect(onDeleted).toHaveBeenCalled()
    vi.useRealTimers()
  })
})
