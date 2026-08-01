import { describe, expect, it } from 'vitest'
import { QueryClient } from '@tanstack/react-query'
import { createAsyncStoragePersister } from '@tanstack/query-async-storage-persister'
import {
  persistQueryClientRestore,
  persistQueryClientSave,
} from '@tanstack/query-persist-client-core'

/**
 * Persister smoke test (US-O.2 Key Decision 1): a fake async storage stands
 * in for IndexedDB — the real `idb-keyval` bindings aren't available in
 * jsdom — and asserts that a persisted query round-trips through
 * save/restore onto a *fresh* QueryClient, i.e. the same mechanism
 * `PersistQueryClientProvider` relies on to hydrate the cache offline.
 */
describe('IndexedDB-style persister', () => {
  it('round-trips a query through save and restore', async () => {
    const store = new Map<string, string>()
    const persister = createAsyncStoragePersister({
      storage: {
        getItem: (key) => Promise.resolve(store.get(key) ?? null),
        setItem: (key, value) => {
          store.set(key, value)
          return Promise.resolve()
        },
        removeItem: (key) => {
          store.delete(key)
          return Promise.resolve()
        },
      },
    })

    const writer = new QueryClient()
    writer.setQueryData(['lists'], [{ id: 'l1', name: 'Groceries' }])
    await persistQueryClientSave({ queryClient: writer, persister, buster: 'v1' })

    expect(store.size).toBeGreaterThan(0)

    const reader = new QueryClient()
    await persistQueryClientRestore({ queryClient: reader, persister, buster: 'v1' })

    expect(reader.getQueryData(['lists'])).toEqual([{ id: 'l1', name: 'Groceries' }])
  })

  it('drops the cache when the buster no longer matches', async () => {
    const store = new Map<string, string>()
    const persister = createAsyncStoragePersister({
      storage: {
        getItem: (key) => Promise.resolve(store.get(key) ?? null),
        setItem: (key, value) => {
          store.set(key, value)
          return Promise.resolve()
        },
        removeItem: (key) => {
          store.delete(key)
          return Promise.resolve()
        },
      },
    })

    const writer = new QueryClient()
    writer.setQueryData(['lists'], [{ id: 'l1', name: 'Groceries' }])
    await persistQueryClientSave({ queryClient: writer, persister, buster: 'old-build' })

    const reader = new QueryClient()
    await persistQueryClientRestore({ queryClient: reader, persister, buster: 'new-build' })

    expect(reader.getQueryData(['lists'])).toBeUndefined()
  })
})
