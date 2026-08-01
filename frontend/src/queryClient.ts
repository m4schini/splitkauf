// The shared React Query client + IndexedDB persister (US-O.2 Key Decision
// 1). Split out from `main.tsx` so modules that only need to *reference* the
// client (e.g. `api.ts`'s `logout()`) can import it without pulling in
// `main.tsx`'s top-level `createRoot(...).render(...)` side effect — that
// side effect throws in any test that doesn't provide a `#root` DOM node.

import { QueryClient } from '@tanstack/react-query'
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
 * doesn't understand. `VITE_BUILD_ID` is set by CI per build; falls back to
 * a fixed string in dev, where schema drift within a single checkout isn't a
 * concern.
 */
export const cacheBuster = import.meta.env.VITE_BUILD_ID ?? 'dev'
