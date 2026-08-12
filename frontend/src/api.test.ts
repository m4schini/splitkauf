import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  addItem,
  apiFetch,
  attributionLabel,
  checkItem,
  createList,
  deleteItem,
  deleteList,
  getAuthConfig,
  getList,
  getMe,
  isUnauthorized,
  listLists,
  login,
  logout,
  passwordLogin,
  renameList,
  restoreItem,
  uncheckItem,
  updateItem,
  type Item,
  type List,
  type ListWithItems,
  type ProblemDetail,
  type User,
} from './api'
import { persister, queryClient } from './queryClient'

function mockResponse(body: unknown, init: { status: number; contentType: string }): Response {
  return {
    ok: init.status >= 200 && init.status < 300,
    status: init.status,
    statusText: init.status === 503 ? 'Service Unavailable' : 'Error',
    headers: new Headers({ 'Content-Type': init.contentType }),
    json: () => Promise.resolve(body),
  } as Response
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('apiFetch', () => {
  it('returns parsed JSON on a 2xx response', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          mockResponse({ status: 'ok' }, { status: 200, contentType: 'application/json' }),
        ),
      ),
    )

    const data = await apiFetch<{ status: string }>('/api/v1/health')
    expect(data).toEqual({ status: 'ok' })
  })

  it('throws the parsed ProblemDetail on application/problem+json', async () => {
    const problem: ProblemDetail = {
      type: '/problems/not-found',
      title: 'Not Found',
      status: 404,
      detail: 'no resource exists at this path',
      instance: '/api/v1/nope',
    }
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          mockResponse(problem, {
            status: 404,
            contentType: 'application/problem+json',
          }),
        ),
      ),
    )

    await expect(apiFetch('/api/v1/nope')).rejects.toEqual(problem)
  })

  it('throws a status/title fallback on a non-problem non-2xx response', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          mockResponse('gateway down', {
            status: 503,
            contentType: 'text/plain',
          }),
        ),
      ),
    )

    await expect(apiFetch('/api/v1/health')).rejects.toEqual({
      status: 503,
      title: 'Service Unavailable',
    })
  })

  it('returns undefined without parsing a body on 204 No Content', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve({
          ok: true,
          status: 204,
          statusText: 'No Content',
          headers: new Headers(),
          json: () => Promise.reject(new Error('should not be called for 204')),
        } as unknown as Response),
      ),
    )

    await expect(apiFetch('/api/v1/lists/1')).resolves.toBeUndefined()
  })
})

describe('endpoint helpers', () => {
  function stubJson(body: unknown, status = 200) {
    const fetchMock = vi.fn(() =>
      Promise.resolve({
        ok: status >= 200 && status < 300,
        status,
        statusText: 'OK',
        headers: new Headers({ 'Content-Type': 'application/json' }),
        json: () => Promise.resolve(body),
      } as Response),
    )
    vi.stubGlobal('fetch', fetchMock)
    return fetchMock
  }

  function stubNoContent() {
    const fetchMock = vi.fn(() =>
      Promise.resolve({
        ok: true,
        status: 204,
        statusText: 'No Content',
        headers: new Headers(),
        json: () => Promise.reject(new Error('should not be called for 204')),
      } as unknown as Response),
    )
    vi.stubGlobal('fetch', fetchMock)
    return fetchMock
  }

  it('getMe fetches /api/v1/me', async () => {
    const user: User = { id: 'u1', name: 'Dev User' }
    const fetchMock = stubJson(user)

    await expect(getMe()).resolves.toEqual(user)
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/me', undefined)
  })

  it('listLists fetches /api/v1/lists', async () => {
    const lists: List[] = [
      {
        id: 'l1',
        name: 'Groceries',
        openItemCount: 1,
        checkedItemCount: 0,
        createdAt: '2026-01-01T00:00:00Z',
        updatedAt: '2026-01-01T00:00:00Z',
      },
    ]
    const fetchMock = stubJson(lists)

    await expect(listLists()).resolves.toEqual(lists)
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/lists', undefined)
  })

  it('createList POSTs a CreateListRequest', async () => {
    const created: List = {
      id: 'l1',
      name: 'Groceries',
      openItemCount: 0,
      checkedItemCount: 0,
      createdAt: '2026-01-01T00:00:00Z',
      updatedAt: '2026-01-01T00:00:00Z',
    }
    const fetchMock = stubJson(created, 201)

    await expect(createList({ name: 'Groceries' })).resolves.toEqual(created)
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/lists', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: 'Groceries' }),
    })
  })

  it('getList fetches /api/v1/lists/{listId}', async () => {
    const list: ListWithItems = {
      id: 'l1',
      name: 'Groceries',
      openItemCount: 0,
      checkedItemCount: 0,
      createdAt: '2026-01-01T00:00:00Z',
      updatedAt: '2026-01-01T00:00:00Z',
      items: [],
    }
    const fetchMock = stubJson(list)

    await expect(getList('l1')).resolves.toEqual(list)
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/lists/l1', undefined)
  })

  it('renameList PATCHes a RenameListRequest', async () => {
    const renamed: List = {
      id: 'l1',
      name: 'New Name',
      openItemCount: 0,
      checkedItemCount: 0,
      createdAt: '2026-01-01T00:00:00Z',
      updatedAt: '2026-01-01T00:00:00Z',
    }
    const fetchMock = stubJson(renamed)

    await expect(renameList('l1', { name: 'New Name' })).resolves.toEqual(renamed)
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/lists/l1', {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: 'New Name' }),
    })
  })

  it('deleteList issues a DELETE and resolves with no body', async () => {
    const fetchMock = stubNoContent()

    await expect(deleteList('l1')).resolves.toBeUndefined()
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/lists/l1', { method: 'DELETE' })
  })

  it('addItem POSTs an AddItemRequest', async () => {
    const item: Item = {
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
    }
    const fetchMock = stubJson(item, 201)

    await expect(addItem('l1', { name: 'Milk' })).resolves.toEqual(item)
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/lists/l1/items', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: 'Milk' }),
    })
  })

  it('updateItem PATCHes an UpdateItemRequest', async () => {
    const item: Item = {
      id: 'i1',
      listId: 'l1',
      name: 'Oat milk',
      quantity: 2,
      unit: 'amount',
      note: null,
      checked: false,
      checkedAt: null,
      createdAt: '2026-01-01T00:00:00Z',
      updatedAt: '2026-01-01T00:00:00Z',
    }
    const fetchMock = stubJson(item)

    await expect(updateItem('l1', 'i1', { name: 'Oat milk', quantity: 2 })).resolves.toEqual(item)
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/lists/l1/items/i1', {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: 'Oat milk', quantity: 2 }),
    })
  })

  it('deleteItem issues a DELETE and resolves with no body', async () => {
    const fetchMock = stubNoContent()

    await expect(deleteItem('l1', 'i1')).resolves.toBeUndefined()
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/lists/l1/items/i1', { method: 'DELETE' })
  })

  it('checkItem POSTs to the check endpoint', async () => {
    const item: Item = {
      id: 'i1',
      listId: 'l1',
      name: 'Milk',
      quantity: 1,
      unit: 'amount',
      note: null,
      checked: true,
      checkedAt: '2026-01-01T00:00:00Z',
      createdAt: '2026-01-01T00:00:00Z',
      updatedAt: '2026-01-01T00:00:00Z',
    }
    const fetchMock = stubJson(item)

    await expect(checkItem('l1', 'i1')).resolves.toEqual(item)
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/lists/l1/items/i1/check', { method: 'POST' })
  })

  it('uncheckItem POSTs to the uncheck endpoint', async () => {
    const item: Item = {
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
    }
    const fetchMock = stubJson(item)

    await expect(uncheckItem('l1', 'i1')).resolves.toEqual(item)
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/lists/l1/items/i1/uncheck', {
      method: 'POST',
    })
  })

  it('restoreItem POSTs to the restore endpoint', async () => {
    const item: Item = {
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
    }
    const fetchMock = stubJson(item)

    await expect(restoreItem('l1', 'i1')).resolves.toEqual(item)
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/lists/l1/items/i1/restore', {
      method: 'POST',
    })
  })

  it('getAuthConfig fetches the auth mode', async () => {
    const fetchMock = stubJson({ mode: 'password' })

    await expect(getAuthConfig()).resolves.toEqual({ mode: 'password' })
    expect(fetchMock).toHaveBeenCalledWith('/api/auth/config', undefined)
  })

  it('passwordLogin POSTs credentials and resolves on 204', async () => {
    const fetchMock = stubNoContent()

    await expect(passwordLogin('alex', 'correct horse')).resolves.toBeUndefined()
    expect(fetchMock).toHaveBeenCalledWith('/api/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username: 'alex', password: 'correct horse' }),
    })
  })
})

describe('isUnauthorized', () => {
  it('is true for a 401 ProblemDetail', () => {
    const problem: ProblemDetail = {
      type: '/problems/unauthorized',
      title: 'Unauthorized',
      status: 401,
    }
    expect(isUnauthorized(problem)).toBe(true)
  })

  it('is true when the type is /problems/unauthorized even without a status', () => {
    expect(isUnauthorized({ type: '/problems/unauthorized' })).toBe(true)
  })

  it('is false for a 404 ProblemDetail', () => {
    const problem: ProblemDetail = {
      type: '/problems/not-found',
      title: 'Not Found',
      status: 404,
    }
    expect(isUnauthorized(problem)).toBe(false)
  })

  it('is false for a non-ProblemDetail error', () => {
    expect(isUnauthorized(new Error('network down'))).toBe(false)
    expect(isUnauthorized(null)).toBe(false)
    expect(isUnauthorized(undefined)).toBe(false)
  })
})

describe('login', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('navigates to the login endpoint with an explicit encoded return_to', () => {
    const location = { href: '', pathname: '/lists/abc', search: '?x=1' }
    vi.stubGlobal('location', location)

    login('/lists/abc?x=1')

    expect(location.href).toBe('/api/auth/login?return_to=%2Flists%2Fabc%3Fx%3D1')
  })

  it('defaults return_to to the current path and query', () => {
    const location = { href: '', pathname: '/lists/abc', search: '?x=1' }
    vi.stubGlobal('location', location)

    login()

    expect(location.href).toBe('/api/auth/login?return_to=%2Flists%2Fabc%3Fx%3D1')
  })
})

describe('logout', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    document.body.innerHTML = ''
  })

  it('submits a top-level POST form to /api/auth/logout', async () => {
    const submit = vi.spyOn(HTMLFormElement.prototype, 'submit').mockImplementation(() => {})

    await logout()

    const form = document.querySelector('form')
    expect(form).not.toBeNull()
    expect(form?.method).toBe('post')
    expect(form?.action).toContain('/api/auth/logout')
    expect(submit).toHaveBeenCalledTimes(1)
  })

  it('clears the in-memory query cache and removes the persisted store before navigating away', async () => {
    const order: string[] = []
    vi.spyOn(HTMLFormElement.prototype, 'submit').mockImplementation(() => {
      order.push('submit')
    })
    queryClient.setQueryData(['me'], { id: 'u1', name: 'Dev User' })
    const clearSpy = vi.spyOn(queryClient, 'clear')
    // Resolve on a later microtask so an unawaited removeClient would let
    // submit run first — the ordering assertion below then catches the bug.
    const removeClientSpy = vi.spyOn(persister, 'removeClient').mockImplementation(async () => {
      await Promise.resolve()
      order.push('removeClient')
    })

    await logout()

    expect(clearSpy).toHaveBeenCalledTimes(1)
    expect(queryClient.getQueryData(['me'])).toBeUndefined()
    expect(removeClientSpy).toHaveBeenCalledTimes(1)
    // The persisted store must be gone before the navigation is kicked off.
    expect(order).toEqual(['removeClient', 'submit'])
  })
})

// US-L.11: the single place that decides how an attribution is addressed. It is
// shared by the overview and the item rows, so its edge cases are pinned here
// rather than duplicated in both component tests.
describe('attributionLabel', () => {
  const meId = 'user-me'

  it('addresses the viewer as "you", by id rather than by name', () => {
    expect(attributionLabel({ id: meId, name: 'Alex' }, meId)).toBe('you')
    // Even with no resolved name: the id alone identifies the viewer.
    expect(attributionLabel({ id: meId, name: null }, meId)).toBe('you')
  })

  it('addresses another member by name', () => {
    expect(attributionLabel({ id: 'user-other', name: 'Maria' }, meId)).toBe('Maria')
  })

  it('returns null when there is nothing worth showing', () => {
    // No attribution at all (a row predating the feature).
    expect(attributionLabel(undefined, meId)).toBeNull()
    // A stranger whose name never resolved — a bare UUID helps nobody.
    expect(attributionLabel({ id: 'user-other', name: null }, meId)).toBeNull()
    expect(attributionLabel({ id: 'user-other', name: '' }, meId)).toBeNull()
  })

  // Before /me resolves there is no viewer to compare against; falling back to
  // the name keeps the row informative instead of blank.
  it('falls back to the name when the viewer is unknown', () => {
    expect(attributionLabel({ id: meId, name: 'Alex' }, undefined)).toBe('Alex')
  })
})
