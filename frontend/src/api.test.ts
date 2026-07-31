import { afterEach, describe, expect, it, vi } from 'vitest'
import { apiFetch, type ProblemDetail } from './api'

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
})
