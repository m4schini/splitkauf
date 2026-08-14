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
      unit: 'amount',
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
      unit: 'amount',
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

    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} onCopied={() => {}} />, {
      wrapper: withQueryClient(),
    })

    expect(await screen.findByText('Open (1)')).toBeInTheDocument()
    expect(screen.getByText('Done (1)')).toBeInTheDocument()
    expect(screen.getByText('Milk')).toBeInTheDocument()
    expect(screen.getByText('Bread')).toBeInTheDocument()

    // Header and item-row actions are icon-only: accessible names stay
    // intact, but the buttons render no visible text.
    expect(screen.getByRole('button', { name: 'Back to lists' })).toHaveTextContent('')
    expect(screen.getByRole('button', { name: 'Copy list' })).toHaveTextContent('')
    expect(screen.getByRole('button', { name: 'Rename list' })).toHaveTextContent('')
    expect(screen.getByRole('button', { name: 'Delete list' })).toHaveTextContent('')
    expect(screen.getByRole('button', { name: 'Edit Milk' })).toHaveTextContent('')
    expect(screen.getByRole('button', { name: 'Remove Milk' })).toHaveTextContent('')
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
                unit: 'amount',
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

    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} onCopied={() => {}} />, {
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

    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} onCopied={() => {}} />, {
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

    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} onCopied={() => {}} />, {
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

    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} onCopied={() => {}} />, {
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

    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} onCopied={() => {}} />, {
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

    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} onCopied={() => {}} />, {
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

  it('adds an item with a quantity and unit and shows "2 l" optimistically', async () => {
    const user = userEvent.setup()
    let postedBody: Record<string, unknown> | undefined
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        if (init?.method === 'POST' && String(input).endsWith('/items')) {
          postedBody = JSON.parse(init.body as string) as Record<string, unknown>
          return Promise.resolve(
            jsonResponse(
              {
                id: 'new-item',
                listId: 'l1',
                name: postedBody.name,
                quantity: postedBody.quantity,
                unit: postedBody.unit,
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

    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} onCopied={() => {}} />, {
      wrapper: withQueryClient(),
    })

    const input = await screen.findByLabelText('Add item')
    await user.type(input, 'Juice')
    // Bump the quantity to 2 and pick litres.
    await user.click(screen.getByRole('button', { name: 'Increase quantity' }))
    await user.selectOptions(screen.getByLabelText('Unit'), 'l')
    await user.click(screen.getByRole('button', { name: 'Add' }))

    // Optimistic row shows quantity + label before the POST resolves.
    expect(await screen.findByText('2 l')).toBeInTheDocument()
    expect(postedBody).toMatchObject({ name: 'Juice', quantity: 2, unit: 'l' })
    // The additive controls reset to 1 × amount for the next add.
    expect(screen.getByTestId('quick-add-quantity')).toHaveTextContent('1')
    expect(screen.getByLabelText('Unit')).toHaveValue('amount')
  })

  it('renders a bare number for the amount unit (no "Stück")', async () => {
    const list: ListWithItems = {
      ...baseList,
      items: [{ ...baseList.items[0], quantity: 3, unit: 'amount' }],
    }
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(jsonResponse(list))),
    )

    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} onCopied={() => {}} />, {
      wrapper: withQueryClient(),
    })

    await screen.findByText('Milk')
    const qty = screen.getByText('3')
    expect(qty).toHaveClass('item-qty')
    // The row shows the bare number only — the "Stück" label is never appended
    // (it exists solely as a unit-selector option).
    expect(screen.getByText('Milk').closest('.item-text')).not.toHaveTextContent('Stück')
  })

  it('does not let the quantity stepper go below 1', async () => {
    const user = userEvent.setup()
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(jsonResponse(baseList))),
    )

    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} onCopied={() => {}} />, {
      wrapper: withQueryClient(),
    })

    await screen.findByText('Milk')
    const decrease = screen.getByRole('button', { name: 'Decrease quantity' })
    // Disabled at the minimum; clicking it can't drop below 1.
    expect(decrease).toBeDisabled()
    await user.click(decrease)
    expect(screen.getByTestId('quick-add-quantity')).toHaveTextContent('1')

    await user.click(screen.getByRole('button', { name: 'Increase quantity' }))
    expect(screen.getByTestId('quick-add-quantity')).toHaveTextContent('2')
    expect(decrease).toBeEnabled()
    await user.click(decrease)
    expect(screen.getByTestId('quick-add-quantity')).toHaveTextContent('1')
    expect(decrease).toBeDisabled()
  })

  it('preserves an item unit through the optimistic check flow', async () => {
    const user = userEvent.setup()
    const list: ListWithItems = {
      ...baseList,
      openItemCount: 1,
      checkedItemCount: 0,
      items: [{ ...baseList.items[0], quantity: 2, unit: 'l', checked: false }],
    }
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        if (String(input).endsWith('/check')) {
          return Promise.resolve(
            jsonResponse({ ...list.items[0], checked: true, checkedAt: '2026-01-01T00:00:00Z' }),
          )
        }
        return Promise.resolve(jsonResponse(list))
      }),
    )

    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} onCopied={() => {}} />, {
      wrapper: withQueryClient(),
    })

    await screen.findByText('Milk')
    expect(screen.getByText('2 l')).toBeInTheDocument()
    await user.click(screen.getByRole('checkbox', { name: /Milk/ }))

    await waitFor(() => expect(screen.getByText('Done (1)')).toBeInTheDocument())
    // The unit survives the check (optimistic toggle preserves the field).
    expect(screen.getByText('2 l')).toBeInTheDocument()
  })

  it('editing an item can change its unit', async () => {
    const user = userEvent.setup()
    let patchBody: Record<string, unknown> | undefined
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        if (init?.method === 'PATCH' && /\/items\/i1$/.test(String(input))) {
          patchBody = JSON.parse(init.body as string) as Record<string, unknown>
          return Promise.resolve(jsonResponse({ ...baseList.items[0], ...patchBody }))
        }
        return Promise.resolve(jsonResponse(baseList))
      }),
    )

    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} onCopied={() => {}} />, {
      wrapper: withQueryClient(),
    })

    await screen.findByText('Milk')
    await user.click(screen.getByRole('button', { name: 'Edit Milk' }))
    await user.selectOptions(screen.getByLabelText('Unit', { selector: '#edit-unit-i1' }), 'l')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(patchBody).toMatchObject({ unit: 'l' }))
    // The row now renders the new unit ("1 l" — quantity unchanged).
    expect(await screen.findByText('1 l')).toBeInTheDocument()
  })

  it('copying the list posts to the copy endpoint and navigates into the copy', async () => {
    const user = userEvent.setup()
    const onCopied = vi.fn()
    const copy = {
      id: 'l2',
      name: 'Groceries (copy)',
      openItemCount: 2,
      checkedItemCount: 0,
      createdAt: '2026-01-02T00:00:00Z',
      updatedAt: '2026-01-02T00:00:00Z',
    }
    const fetchMock = vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === 'POST') return Promise.resolve(jsonResponse(copy, 201))
      return Promise.resolve(jsonResponse(baseList))
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} onCopied={onCopied} />, {
      wrapper: withQueryClient(),
    })

    await screen.findByText('Milk')
    await user.click(screen.getByRole('button', { name: 'Copy list' }))

    await waitFor(() => expect(onCopied).toHaveBeenCalledWith('l2'))

    const [url, init] = fetchMock.mock.calls.find(([, i]) => i?.method === 'POST')!
    expect(url).toBe('/api/v1/lists/l1/copy')
    // No body (and therefore no content type): that's how the server is asked
    // to derive the "«Name» (copy)" name.
    expect(init?.body).toBeUndefined()
  })

  it('shows a hint and stays put when the copy fails (e.g. offline)', async () => {
    const user = userEvent.setup()
    const onCopied = vi.fn()
    vi.stubGlobal(
      'fetch',
      vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
        if (init?.method === 'POST') return Promise.reject(new TypeError('Failed to fetch'))
        return Promise.resolve(jsonResponse(baseList))
      }),
    )

    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} onCopied={onCopied} />, {
      wrapper: withQueryClient(),
    })

    await screen.findByText('Milk')
    await user.click(screen.getByRole('button', { name: 'Copy list' }))

    expect(await screen.findByText("Couldn't copy the list.")).toBeInTheDocument()
    expect(onCopied).not.toHaveBeenCalled()
    // Non-blocking: the list itself is still on screen and usable.
    expect(screen.getByText('Milk')).toBeInTheDocument()
  })

  it('disables the copy button while the copy is in flight', async () => {
    const user = userEvent.setup()
    let resolveCopy: ((value: Response) => void) | undefined
    vi.stubGlobal(
      'fetch',
      vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
        if (init?.method === 'POST') {
          return new Promise<Response>((resolve) => {
            resolveCopy = resolve
          })
        }
        return Promise.resolve(jsonResponse(baseList))
      }),
    )

    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} onCopied={() => {}} />, {
      wrapper: withQueryClient(),
    })

    await screen.findByText('Milk')
    const button = screen.getByRole('button', { name: 'Copy list' })
    await user.click(button)

    await waitFor(() => expect(button).toBeDisabled())

    resolveCopy?.(
      jsonResponse(
        {
          id: 'l2',
          name: 'Groceries (copy)',
          openItemCount: 2,
          checkedItemCount: 0,
          createdAt: '2026-01-02T00:00:00Z',
          updatedAt: '2026-01-02T00:00:00Z',
        },
        201,
      ),
    )
    await waitFor(() => expect(button).toBeEnabled())
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

    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={onDeleted} onCopied={() => {}} />, {
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

// US-L.11: an open item says who put it on the list, a checked one says who
// bought it. Both are resolved against /me so the viewer reads as "you".
describe('ListDetail item attribution', () => {
  const me = { id: 'user-me', name: 'Alex' }

  /** Serves /me and the list detail separately so id comparison is meaningful. */
  function stubApi(list: ListWithItems) {
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) =>
        Promise.resolve(jsonResponse(String(input).endsWith('/me') ? me : list)),
      ),
    )
  }

  function listWithItems(items: ListWithItems['items']): ListWithItems {
    return { ...baseList, items }
  }

  function item(overrides: Partial<ListWithItems['items'][number]>) {
    return { ...baseList.items[0], ...overrides }
  }

  it('credits the adder on an open item and the buyer on a checked one', async () => {
    stubApi(
      listWithItems([
        item({ id: 'i1', name: 'Milk', addedBy: { id: 'user-other', name: 'Maria' } }),
        item({
          id: 'i2',
          name: 'Bread',
          checked: true,
          checkedAt: '2026-01-01T00:00:00Z',
          addedBy: { id: 'user-other', name: 'Maria' },
          boughtBy: { id: me.id, name: 'Alex' },
        }),
      ]),
    )

    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} onCopied={() => {}} />, {
      wrapper: withQueryClient(),
    })

    expect(await screen.findByText('Added by Maria')).toBeInTheDocument()
    // The checked row shows the buyer, NOT its adder — who bought it is the
    // useful fact once it is in the cart.
    expect(screen.getByText('Bought by you')).toBeInTheDocument()
    expect(screen.queryByText('Added by you')).not.toBeInTheDocument()
  })

  it('shows no attribution for items that predate it', async () => {
    stubApi(listWithItems([item({ id: 'i1', name: 'Milk' })]))

    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} onCopied={() => {}} />, {
      wrapper: withQueryClient(),
    })

    await screen.findByText('Milk')
    expect(screen.queryByText(/Added by/)).not.toBeInTheDocument()
    expect(screen.queryByText(/Bought by/)).not.toBeInTheDocument()
  })

  it('attributes an optimistically added item to the viewer', async () => {
    const user = userEvent.setup()
    // The POST never settles, so only the optimistic row can be asserted.
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        if (init?.method === 'POST') return new Promise<Response>(() => {})
        return Promise.resolve(jsonResponse(String(input).endsWith('/me') ? me : listWithItems([])))
      }),
    )

    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} onCopied={() => {}} />, {
      wrapper: withQueryClient(),
    })

    await user.type(await screen.findByLabelText('Add item'), 'Eggs')
    await user.click(screen.getByRole('button', { name: 'Add' }))

    expect(await screen.findByText('Added by you')).toBeInTheDocument()
  })

  // Checking optimistically flips the line from adder to buyer, and unchecking
  // must clear the buyer rather than leave a stale "Bought by" on an open item.
  it('flips to "Bought by you" on check and back on uncheck', async () => {
    const user = userEvent.setup()
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        if (init?.method === 'POST') return new Promise<Response>(() => {})
        return Promise.resolve(
          jsonResponse(
            String(input).endsWith('/me')
              ? me
              : listWithItems([
                  item({ id: 'i1', name: 'Milk', addedBy: { id: me.id, name: 'Alex' } }),
                ]),
          ),
        )
      }),
    )

    render(<ListDetail listId="l1" onBack={() => {}} onDeleted={() => {}} onCopied={() => {}} />, {
      wrapper: withQueryClient(),
    })

    expect(await screen.findByText('Added by you')).toBeInTheDocument()

    await user.click(screen.getByRole('checkbox'))
    expect(await screen.findByText('Bought by you')).toBeInTheDocument()

    await user.click(screen.getByRole('checkbox'))
    expect(await screen.findByText('Added by you')).toBeInTheDocument()
    expect(screen.queryByText('Bought by you')).not.toBeInTheDocument()
  })
})
