import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { AppEvent } from './live'

type Handler = (event: Event | MessageEvent) => void

/**
 * A fake EventSource that records listeners per event type and lets tests
 * fire synthetic `message`/`error`/`open` events, mirroring the real
 * browser API surface `live.ts` depends on.
 */
class FakeEventSource {
  static instances: FakeEventSource[] = []
  url: string
  closed = false
  private listeners = new Map<string, Set<Handler>>()

  constructor(url: string) {
    this.url = url
    FakeEventSource.instances.push(this)
  }

  addEventListener(type: string, handler: Handler): void {
    const set = this.listeners.get(type) ?? new Set()
    set.add(handler)
    this.listeners.set(type, set)
  }

  removeEventListener(type: string, handler: Handler): void {
    this.listeners.get(type)?.delete(handler)
  }

  close(): void {
    this.closed = true
  }

  emit(type: string, event: unknown = {}): void {
    for (const handler of this.listeners.get(type) ?? []) handler(event as Event)
  }
}

function lastInstance(): FakeEventSource {
  const instance = FakeEventSource.instances.at(-1)
  if (!instance) throw new Error('expected an EventSource to have been constructed')
  return instance
}

beforeEach(() => {
  FakeEventSource.instances = []
  vi.stubGlobal('EventSource', FakeEventSource)
  // The module holds singleton state (the shared connection, listener set);
  // reset it between tests so subscriptions from one test don't leak into
  // the next.
  vi.resetModules()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('live', () => {
  it('opens exactly one connection for multiple subscribers', async () => {
    const { subscribeLive } = await import('./live')
    const handler1 = vi.fn()
    const handler2 = vi.fn()

    subscribeLive(handler1)
    subscribeLive(handler2)

    expect(FakeEventSource.instances).toHaveLength(1)
  })

  it('dispatches a parsed AppEvent from a message frame to all listeners', async () => {
    const { subscribeLive } = await import('./live')
    const handler1 = vi.fn()
    const handler2 = vi.fn()
    subscribeLive(handler1)
    subscribeLive(handler2)

    const payload: AppEvent = { type: 'items', listId: 'list-1' }
    lastInstance().emit('message', { data: JSON.stringify(payload) })

    expect(handler1).toHaveBeenCalledExactlyOnceWith(payload)
    expect(handler2).toHaveBeenCalledExactlyOnceWith(payload)
  })

  it('keeps the connection open until the last subscriber unsubscribes, then closes it', async () => {
    const { subscribeLive } = await import('./live')
    const unsubscribe1 = subscribeLive(vi.fn())
    const unsubscribe2 = subscribeLive(vi.fn())
    const instance = lastInstance()

    unsubscribe1()
    expect(instance.closed).toBe(false)

    unsubscribe2()
    expect(instance.closed).toBe(true)
  })

  it('reopens a fresh connection once a new subscriber arrives after the last unsubscribed', async () => {
    const { subscribeLive } = await import('./live')
    const unsubscribe = subscribeLive(vi.fn())
    unsubscribe()
    expect(FakeEventSource.instances).toHaveLength(1)

    subscribeLive(vi.fn())

    expect(FakeEventSource.instances).toHaveLength(2)
  })

  it('emits a synthetic reconnect event once when the connection reopens after an error', async () => {
    const { subscribeLive } = await import('./live')
    const handler = vi.fn()
    subscribeLive(handler)
    const instance = lastInstance()

    instance.emit('error')
    expect(handler).not.toHaveBeenCalled()

    instance.emit('open')
    expect(handler).toHaveBeenCalledExactlyOnceWith({ type: 'reconnect' })
  })

  it('does not emit reconnect on the initial open (only after a prior error)', async () => {
    const { subscribeLive } = await import('./live')
    const handler = vi.fn()
    subscribeLive(handler)
    const instance = lastInstance()

    instance.emit('open')

    expect(handler).not.toHaveBeenCalled()
  })

  it('ignores malformed message frames without throwing', async () => {
    const { subscribeLive } = await import('./live')
    const handler = vi.fn()
    subscribeLive(handler)
    const instance = lastInstance()

    expect(() => instance.emit('message', { data: 'not json' })).not.toThrow()
    expect(handler).not.toHaveBeenCalled()
  })
})
