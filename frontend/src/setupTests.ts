import '@testing-library/jest-dom/vitest'

/**
 * jsdom has no EventSource implementation. Views use `live.ts`'s shared
 * EventSource singleton via `useLiveEvents`, so any component test that
 * renders ListsOverview/ListDetail (directly or via App) needs one to exist
 * globally, or the `new EventSource(...)` call throws. This stub never
 * touches the network and never emits events — it's just inert plumbing.
 * `live.test.ts` installs its own richer fake (via `vi.stubGlobal`, which
 * `vi.unstubAllGlobals()` restores back to this one) to exercise real
 * open/error/message/reconnect behavior.
 */
class NoopEventSource {
  static readonly CONNECTING = 0
  static readonly OPEN = 1
  static readonly CLOSED = 2
  readyState = NoopEventSource.CONNECTING
  constructor(_url: string | URL) {}
  addEventListener(): void {}
  removeEventListener(): void {}
  dispatchEvent(): boolean {
    return true
  }
  close(): void {}
}

globalThis.EventSource = NoopEventSource as unknown as typeof EventSource
