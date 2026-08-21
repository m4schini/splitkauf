// The shared React Query client + IndexedDB persister (US-O.2 Key Decision
// 1). Split out from `main.tsx` so modules that only need to *reference* the
// client (e.g. `api.ts`'s `logout()`) can import it without pulling in
// `main.tsx`'s top-level `createRoot(...).render(...)` side effect — that
// side effect throws in any test that doesn't provide a `#root` DOM node.

import {
  QueryClient,
  defaultShouldDehydrateQuery,
  type DehydrateOptions,
} from '@tanstack/react-query'
import { createAsyncStoragePersister } from '@tanstack/query-async-storage-persister'
import { get, set, del } from 'idb-keyval'

const oneDayMs = 24 * 60 * 60 * 1000
export const sevenDaysMs = 7 * oneDayMs

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // The core loop is a single-user PWA hitting a local-ish API; avoid
      // surprise refetches that would fight optimistic updates mid-edit.
      refetchOnWindowFocus: false,
      retry: 1,
      // Persistence cannot outlive garbage collection: gcTime must be >= the
      // persister's maxAge below, or entries are evicted from memory before
      // they'd ever be read back from IndexedDB (US-O.2 offline reads).
      gcTime: sevenDaysMs,
    },
  },
})

/** Async IndexedDB storage for the persisted query cache (US-O.2). */
export const persister = createAsyncStoragePersister({
  storage: {
    getItem: get,
    setItem: set,
    removeItem: del,
  },
})

/**
 * Invalidates the persisted cache when the app build changes, so a client
 * left open across a deploy never hydrates a schema an older/newer build
 * doesn't understand. `VITE_BUILD_ID` is the git SHA, injected per build by
 * the Dockerfile (via CI) and the Makefile's frontend-build rule; `||` (not
 * `??`) so an empty value also falls back to the fixed dev string, where
 * schema drift within a single checkout isn't a concern.
 */
export const cacheBuster = import.meta.env.VITE_BUILD_ID || 'dev'

/** Query key for GET /api/auth/config (shared with App.tsx). */
export const authConfigKey = ['authConfig'] as const

/**
 * Dehydration filter for the persisted cache: keep the paused-mutation outbox
 * (US-O.2 Key Decision 2) and every successful query EXCEPT the auth-mode
 * lookup. The auth mode is cached for the lifetime of a page (staleTime:
 * Infinity) but must be re-asked on every fresh load — persisting it froze
 * the signed-out UI in the old mode for up to maxAge days after a deploy
 * switched the server from password to OIDC.
 */
export const dehydrateOptions: DehydrateOptions = {
  shouldDehydrateMutation: (mutation) => mutation.state.isPaused,
  shouldDehydrateQuery: (query) =>
    defaultShouldDehydrateQuery(query) && query.queryKey[0] !== authConfigKey[0],
}

/**
 * Replays the persisted paused-mutation outbox (US-O.3). Called when the app
 * regains connectivity (`online`) or is brought back to the foreground
 * (`visibilitychange` → visible, the iOS-friendly substitute for the absent
 * Background Sync API), and after the cache is restored on startup (wired in
 * `main.tsx`). Reads `queryClient.resumePausedMutations` at call time so tests
 * can spy on it.
 */
function resumePausedMutations(): void {
  void queryClient.resumePausedMutations()
}

/** Registers the outbox resume triggers. Idempotent-enough for a singleton. */
export function registerResumeTriggers(): void {
  if (typeof window === 'undefined') return
  window.addEventListener('online', resumePausedMutations)
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') resumePausedMutations()
  })
}

registerResumeTriggers()
