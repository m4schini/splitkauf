import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  addItem,
  apiFetch,
  checkItem,
  createList,
  deleteItem,
  deleteList,
  getList,
  getMe,
  isUnauthorized,
  listLists,
  login,
  logout,
  renameList,
  uncheckItem,
  updateItem,
  type Item,
  type List,
  type ListWithItems,
  type ProblemDetail,
  type User,
} from './api'

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

  it('submits a top-level POST form to /api/auth/logout', () => {
    const submit = vi.spyOn(HTMLFormElement.prototype, 'submit').mockImplementation(() => {})

    logout()

    const form = document.querySelector('form')
    expect(form).not.toBeNull()
    expect(form?.method).toBe('post')
    expect(form?.action).toContain('/api/auth/logout')
    expect(submit).toHaveBeenCalledTimes(1)
  })
})
