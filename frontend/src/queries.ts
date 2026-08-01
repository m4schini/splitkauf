// React Query hooks for lists & items. Every mutation applies an optimistic
// update via `onMutate` and rolls back via `onError`, per architecture.md §3,
// so the UI never blocks on a network round-trip for the core add/check loop.
//
// Offline outbox (US-O.2 Key Decision 2): every mutation runs with
// `networkMode: 'offlineFirst'` PLUS a retry policy. In RQ v5 `offlineFirst`
// fires the mutationFn once and only *pauses* on retry, so with the default
// `retry: 0` an offline mutation would fail straight into the `onError`
// rollback instead of queuing. `mutationRetry` pauses on network failures
// (they replay from the persisted outbox) and fails fast on 4xx problems (no
// pointless replays); `onError` therefore fires only on a *final* failure,
// never on a pause. Persisted paused mutations resume after a reload, when no
// component is mounted to supply options, so each operation registers a stable
// `mutationKey` + default options via `setMutationDefaults` at module scope.
import {
  onlineManager,
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
  type UseMutationOptions,
} from '@tanstack/react-query'
import {
  addItem,
  checkItem,
  createList,
  deleteItem as apiDeleteItem,
  deleteList as apiDeleteList,
  getList,
  listLists,
  renameList,
  restoreItem as apiRestoreItem,
  uncheckItem,
  updateItem,
  type AddItemRequest,
  type Item,
  type List,
  type ListWithItems,
  type ProblemDetail,
  type UpdateItemRequest,
} from './api'
import { queryClient as sharedQueryClient } from './queryClient'

export const listsKey = ['lists'] as const
export const listKey = (listId: string) => ['lists', listId] as const

// Stable mutation keys: the identity a persisted paused mutation resumes under.
export const addItemKey = ['items', 'add'] as const
export const updateItemKey = ['items', 'update'] as const
export const checkItemKey = ['items', 'check'] as const
export const uncheckItemKey = ['items', 'uncheck'] as const
export const deleteItemKey = ['items', 'delete'] as const
export const restoreItemKey = ['items', 'restore'] as const
export const createListKey = ['lists', 'create'] as const
export const renameListKey = ['lists', 'rename'] as const
export const deleteListKey = ['lists', 'delete'] as const

export function useLists() {
  return useQuery({ queryKey: listsKey, queryFn: listLists })
}

export function useListDetail(listId: string) {
  return useQuery({ queryKey: listKey(listId), queryFn: () => getList(listId) })
}

/** Patches the list summary (counts/name) in the overview cache, if present. */
function patchListSummary(queryClient: QueryClient, listId: string, patch: Partial<List>) {
  queryClient.setQueryData<List[]>(listsKey, (old) =>
    old?.map((l) => (l.id === listId ? { ...l, ...patch } : l)),
  )
}

function nowIso(): string {
  return new Date().toISOString()
}

/**
 * Generates the id for an optimistic create: `temp-<uuid>`. The `temp-` prefix
 * is the structural marker of an unsynced create — it survives a reload in the
 * persisted cache and drives the unsynced badge. Uses `crypto.randomUUID()`
 * when available, falling back to a `Math.random`-based id on a plain-HTTP
 * self-host where `crypto.randomUUID` is absent; it must never throw.
 */
export function randomId(): string {
  const uuid =
    typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function'
      ? crypto.randomUUID()
      : `${Date.now().toString(16)}-${Math.random().toString(16).slice(2)}-${Math.random()
          .toString(16)
          .slice(2)}`
  return `temp-${uuid}`
}

/** True for an optimistic id minted by `randomId` — a not-yet-synced create. */
function isTempId(id: string): boolean {
  return id.startsWith('temp-')
}

// --- Error classification (US-O.2 Key Decision 2 / replay-404 policy) -------

/**
 * True when `err` is a `ProblemDetail` the outbox must NOT replay: a client
 * error with a numeric HTTP status in [400, 500). A status-less problem is
 * treated as NOT 4xx (unknown → pause/retry) so a `/problems/internal` with no
 * status doesn't fail fast; a 5xx (server/transient) is likewise excluded so it
 * keeps pausing/retrying rather than failing fast.
 */
export function is4xxProblem(err: unknown): boolean {
  if (typeof err !== 'object' || err === null) return false
  const problem = err as ProblemDetail
  if (typeof problem.status !== 'number') return false
  return problem.status >= 400 && problem.status < 500
}

const networkMode = 'offlineFirst' as const
function mutationRetry(failureCount: number, err: unknown): boolean {
  return !is4xxProblem(err) && failureCount < 3
}

// --- Quiet sync-failure notice (replay-404) ---------------------------------

type SyncNoticeListener = (message: string) => void
const syncNoticeListeners = new Set<SyncNoticeListener>()

/** Subscribes to quiet sync-failure notices; returns an unsubscribe fn. */
export function subscribeSyncNotice(listener: SyncNoticeListener): () => void {
  syncNoticeListeners.add(listener)
  return () => {
    syncNoticeListeners.delete(listener)
  }
}

const SYNC_FAILED_MESSAGE = "Some offline changes couldn't sync"
function emitSyncNotice(message: string = SYNC_FAILED_MESSAGE): void {
  syncNoticeListeners.forEach((listener) => listener(message))
}

/**
 * Shared final-error handling. Any FINAL 4xx (the target is gone, a validation
 * error, etc.) means a replayed change the server rejects: per US-O.3 it is
 * dropped (not re-queued) — invalidate the affected keys and surface a quiet
 * notice rather than rolling back silently. Non-4xx (network/5xx) errors are
 * only ever seen here after retries are exhausted: roll back precisely when a
 * pre-mutation snapshot survives (it won't after a reload, where `onMutate`'s
 * context is lost), otherwise refetch to reconcile with server truth.
 */
function handleFinalError(
  queryClient: QueryClient,
  err: unknown,
  listId: string | undefined,
  rollback?: () => boolean,
): void {
  if (is4xxProblem(err)) {
    if (listId) queryClient.invalidateQueries({ queryKey: listKey(listId) })
    queryClient.invalidateQueries({ queryKey: listsKey })
    // TODO(US-O.3): debounce/aggregate the notice when many mutations fail at
    // once (a whole outbox replaying against stale ids), rather than one toast
    // per failure.
    emitSyncNotice()
    return
  }
  if (rollback && rollback()) return
  if (listId) queryClient.invalidateQueries({ queryKey: listKey(listId) })
  queryClient.invalidateQueries({ queryKey: listsKey })
}

// --- Mutation variables & optimistic-context shapes -------------------------

export interface AddItemVariables {
  listId: string
  tempId: string
  payload: AddItemRequest
}
interface ItemVariables {
  listId: string
  itemId: string
}
interface UpdateItemVariables {
  listId: string
  itemId: string
  body: UpdateItemRequest
}
interface RestoreVariables {
  listId: string
  item: Item
}
interface RenameVariables {
  listId: string
  name: string
}
interface CreateListVariables {
  name: string
  tempId: string
}
interface DeleteListVariables {
  listId: string
}

// --- Optimistic cache helpers -----------------------------------------------

function toggleChecked(queryClient: QueryClient, listId: string, itemId: string, checked: boolean) {
  const previous = queryClient.getQueryData<ListWithItems>(listKey(listId))
  queryClient.setQueryData<ListWithItems>(
    listKey(listId),
    (old) =>
      old && {
        ...old,
        items: old.items.map((i) =>
          i.id === itemId ? { ...i, checked, checkedAt: checked ? nowIso() : null } : i,
        ),
        openItemCount: old.openItemCount + (checked ? -1 : 1),
        checkedItemCount: old.checkedItemCount + (checked ? 1 : -1),
      },
  )
  patchListSummary(queryClient, listId, {
    openItemCount: (previous?.openItemCount ?? 0) + (checked ? -1 : 1),
    checkedItemCount: (previous?.checkedItemCount ?? 0) + (checked ? 1 : -1),
  })
  return previous
}

// --- Paused-create fold + temp→real reconciliation --------------------------
//
// The RQ mutation cache is the SINGLE source of truth for a pending create: a
// paused add-item mutation, with its `state.variables` (which the persister
// dehydrates), is the sole record of the create and its latest payload. There
// is no separate in-memory registry, so folds and reconciliation survive a
// reload.

/**
 * Finds a still-QUEUED (unsynced) add-item create for `tempId`, if any. A
 * create is queued — and therefore foldable — while it has not reached the
 * server: it is paused (RQ marked it after its retry backoff elapsed offline),
 * OR we are currently offline (its in-flight attempt fails and retries, so the
 * eventual POST re-reads the merged `variables.payload`). An ONLINE, non-paused
 * pending create is genuinely in-flight and is deliberately NOT matched — the
 * caller falls through to a normal mutation and relies on the temp→real remap.
 */
function findPausedCreate(queryClient: QueryClient, tempId: string) {
  return queryClient.getMutationCache().find({
    mutationKey: addItemKey,
    predicate: (m) => {
      if ((m.state.variables as AddItemVariables | undefined)?.tempId !== tempId) return false
      return m.state.isPaused || !onlineManager.isOnline()
    },
  })
}

/**
 * Merges `merge` into a PAUSED create's payload IN PLACE so an offline
 * edit/check on a still-queued create rides out on the create's single eventual
 * POST (Key Decision 3) — and survives a reload, since the persister dehydrates
 * `state.variables`. Returns true when such a paused create exists.
 *
 * NOTE: mutating `mutation.state.variables` is not part of the public
 * @tanstack/react-query API. This helper (and `remapQueuedItemId`) are the ONLY
 * places the outbox writes it; a library upgrade adapts here.
 */
function updatePausedCreatePayload(
  queryClient: QueryClient,
  tempId: string,
  merge: Partial<AddItemRequest>,
): boolean {
  const paused = findPausedCreate(queryClient, tempId)
  if (!paused) return false
  const vars = paused.state.variables as AddItemVariables
  vars.payload = { ...vars.payload, ...merge }
  return true
}

/**
 * On create success, rewrites any QUEUED item mutations whose variables still
 * reference the temp id to the real server id, so a check/edit/delete that fell
 * through during the create's in-flight window targets the real server row
 * instead of 404ing (Key Decision 4). Writes `state.variables` — see the NOTE
 * on `updatePausedCreatePayload`.
 */
function remapQueuedItemId(queryClient: QueryClient, tempId: string, realId: string): void {
  queryClient
    .getMutationCache()
    .getAll()
    .forEach((m) => {
      const vars = m.state.variables as { itemId?: string } | undefined
      if (vars && vars.itemId === tempId) vars.itemId = realId
    })
}

// --- Default option factories (used inline in hooks AND for resume) ---------

function buildAddItemDefaults(
  queryClient: QueryClient,
): UseMutationOptions<
  Item,
  unknown,
  AddItemVariables,
  { previous?: ListWithItems; previousLists?: List[] }
> {
  return {
    networkMode,
    retry: mutationRetry,
    // The paused mutation's own `variables.payload` is the single source of
    // truth for the (possibly folded) create — read it directly (Key Decision
    // 2/3). Folds mutate it in place via `updatePausedCreatePayload`, and the
    // persister dehydrates it, so this carries the merged payload after a
    // reload too.
    mutationFn: ({ listId, payload }) => addItem(listId, payload),
    onMutate: async ({ listId, tempId, payload }) => {
      await queryClient.cancelQueries({ queryKey: listKey(listId) })
      const previous = queryClient.getQueryData<ListWithItems>(listKey(listId))
      const previousLists = queryClient.getQueryData<List[]>(listsKey)
      const checked = payload.checked ?? false
      const optimistic: Item = {
        id: tempId,
        listId,
        name: payload.name,
        quantity: payload.quantity ?? 1,
        note: payload.note ?? null,
        checked,
        checkedAt: checked ? nowIso() : null,
        createdAt: nowIso(),
        updatedAt: nowIso(),
      }
      queryClient.setQueryData<ListWithItems>(
        listKey(listId),
        (old) =>
          old && {
            ...old,
            items: [...old.items, optimistic],
            openItemCount: old.openItemCount + (checked ? 0 : 1),
            checkedItemCount: old.checkedItemCount + (checked ? 1 : 0),
          },
      )
      patchListSummary(queryClient, listId, {
        openItemCount: (previous?.openItemCount ?? 0) + (checked ? 0 : 1),
        checkedItemCount: (previous?.checkedItemCount ?? 0) + (checked ? 1 : 0),
      })
      return { previous, previousLists }
    },
    onError: (err, { listId }, context) => {
      handleFinalError(queryClient, err, listId, () => {
        if (!context?.previous) return false
        queryClient.setQueryData(listKey(listId), context.previous)
        if (context.previousLists) queryClient.setQueryData(listsKey, context.previousLists)
        return true
      })
    },
    onSuccess: (created, { listId, tempId }) => {
      // Replace the temp item with the server item in the detail cache. The
      // overview cache holds only counts (already bumped optimistically), so
      // there is no temp row to swap there.
      queryClient.setQueryData<ListWithItems>(
        listKey(listId),
        (old) =>
          old && {
            ...old,
            items: old.items.map((i) => (i.id === tempId ? created : i)),
          },
      )
      // Reconcile any check/edit/delete that fell through during the create's
      // in-flight window from the temp id to the real server id (Key Decision 4).
      remapQueuedItemId(queryClient, tempId, created.id)
    },
  }
}

function buildUpdateItemDefaults(
  queryClient: QueryClient,
): UseMutationOptions<Item, unknown, UpdateItemVariables, { previous?: ListWithItems }> {
  return {
    networkMode,
    retry: mutationRetry,
    mutationFn: ({ listId, itemId, body }) => updateItem(listId, itemId, body),
    onMutate: async ({ listId, itemId, body }) => {
      await queryClient.cancelQueries({ queryKey: listKey(listId) })
      const previous = queryClient.getQueryData<ListWithItems>(listKey(listId))
      queryClient.setQueryData<ListWithItems>(
        listKey(listId),
        (old) =>
          old && {
            ...old,
            items: old.items.map((i) => (i.id === itemId ? { ...i, ...body } : i)),
          },
      )
      return { previous }
    },
    onError: (err, { listId }, context) =>
      handleFinalError(queryClient, err, listId, () => {
        if (!context?.previous) return false
        queryClient.setQueryData(listKey(listId), context.previous)
        return true
      }),
  }
}

function buildCheckDefaults(
  queryClient: QueryClient,
  checked: boolean,
): UseMutationOptions<Item, unknown, ItemVariables, { previous?: ListWithItems }> {
  return {
    networkMode,
    retry: mutationRetry,
    mutationFn: ({ listId, itemId }) =>
      checked ? checkItem(listId, itemId) : uncheckItem(listId, itemId),
    onMutate: async ({ listId, itemId }) => {
      await queryClient.cancelQueries({ queryKey: listKey(listId) })
      const previous = toggleChecked(queryClient, listId, itemId, checked)
      return { previous }
    },
    onError: (err, { listId }, context) =>
      handleFinalError(queryClient, err, listId, () => {
        if (!context?.previous) return false
        queryClient.setQueryData(listKey(listId), context.previous)
        return true
      }),
  }
}

function buildDeleteItemDefaults(
  queryClient: QueryClient,
): UseMutationOptions<void, unknown, ItemVariables> {
  return {
    networkMode,
    retry: mutationRetry,
    // The caller removes the item from the cache optimistically before this
    // fires (see ListDetail.handleRemoveItem), so there is no onMutate here.
    mutationFn: ({ listId, itemId }) => apiDeleteItem(listId, itemId),
    onError: (err) => {
      if (is4xxProblem(err)) emitSyncNotice()
    },
    onSettled: (_data, _err, { listId }) => {
      queryClient.invalidateQueries({ queryKey: listKey(listId) })
      queryClient.invalidateQueries({ queryKey: listsKey })
    },
  }
}

function buildRestoreItemDefaults(
  queryClient: QueryClient,
): UseMutationOptions<Item, unknown, RestoreVariables, { previous?: ListWithItems }> {
  return {
    networkMode,
    retry: mutationRetry,
    mutationFn: ({ listId, item }) => apiRestoreItem(listId, item.id),
    onMutate: async ({ listId, item }) => {
      await queryClient.cancelQueries({ queryKey: listKey(listId) })
      const previous = queryClient.getQueryData<ListWithItems>(listKey(listId))
      restoreItemLocally(queryClient, listId, item)
      return { previous }
    },
    onError: (err, { listId }, context) =>
      handleFinalError(queryClient, err, listId, () => {
        if (!context?.previous) return false
        queryClient.setQueryData(listKey(listId), context.previous)
        return true
      }),
    onSuccess: (restored, { listId }) => {
      queryClient.setQueryData<ListWithItems>(
        listKey(listId),
        (old) =>
          old && {
            ...old,
            // Upsert rather than a pure replace: a concurrent delete-triggered
            // refetch can land between onMutate and onSuccess and drop the item
            // from the cache, so `restored.id` may no longer be present.
            items: old.items.some((i) => i.id === restored.id)
              ? old.items.map((i) => (i.id === restored.id ? restored : i))
              : [...old.items, restored],
          },
      )
    },
    onSettled: (_data, _err, { listId }) => {
      queryClient.invalidateQueries({ queryKey: listKey(listId) })
    },
  }
}

function buildCreateListDefaults(
  queryClient: QueryClient,
): UseMutationOptions<List, unknown, CreateListVariables, { previous?: List[] }> {
  return {
    networkMode,
    retry: mutationRetry,
    mutationFn: ({ name }) => createList({ name }),
    onMutate: async ({ name, tempId }) => {
      await queryClient.cancelQueries({ queryKey: listsKey })
      const previous = queryClient.getQueryData<List[]>(listsKey)
      const optimistic: List = {
        id: tempId,
        name,
        openItemCount: 0,
        checkedItemCount: 0,
        createdAt: nowIso(),
        updatedAt: nowIso(),
      }
      queryClient.setQueryData<List[]>(listsKey, (old) => [...(old ?? []), optimistic])
      return { previous }
    },
    onError: (err, _vars, context) =>
      handleFinalError(queryClient, err, undefined, () => {
        if (!context?.previous) return false
        queryClient.setQueryData(listsKey, context.previous)
        return true
      }),
    onSuccess: (created, { tempId }) => {
      queryClient.setQueryData<List[]>(listsKey, (old) =>
        old?.map((l) => (l.id === tempId ? created : l)),
      )
    },
  }
}

function buildRenameListDefaults(
  queryClient: QueryClient,
): UseMutationOptions<
  List,
  unknown,
  RenameVariables,
  { previousDetail?: ListWithItems; previousLists?: List[] }
> {
  return {
    networkMode,
    retry: mutationRetry,
    mutationFn: ({ listId, name }) => renameList(listId, { name }),
    onMutate: async ({ listId, name }) => {
      await queryClient.cancelQueries({ queryKey: listKey(listId) })
      const previousDetail = queryClient.getQueryData<ListWithItems>(listKey(listId))
      const previousLists = queryClient.getQueryData<List[]>(listsKey)
      queryClient.setQueryData<ListWithItems>(listKey(listId), (old) => old && { ...old, name })
      patchListSummary(queryClient, listId, { name })
      return { previousDetail, previousLists }
    },
    onError: (err, { listId }, context) =>
      handleFinalError(queryClient, err, listId, () => {
        if (!context?.previousDetail && !context?.previousLists) return false
        if (context?.previousDetail)
          queryClient.setQueryData(listKey(listId), context.previousDetail)
        if (context?.previousLists) queryClient.setQueryData(listsKey, context.previousLists)
        return true
      }),
  }
}

function buildDeleteListDefaults(
  queryClient: QueryClient,
): UseMutationOptions<void, unknown, DeleteListVariables> {
  return {
    networkMode,
    retry: mutationRetry,
    mutationFn: ({ listId }) => apiDeleteList(listId),
    onError: (err) => {
      if (is4xxProblem(err)) emitSyncNotice()
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: listsKey })
    },
  }
}

/**
 * Registers every mutation's default options on `client` under its stable
 * mutationKey, so persisted paused mutations resume after a reload (when no
 * hook is mounted to supply options). Called for the shared app client at
 * module load; tests call it on a fresh client to exercise resume-after-reload.
 */
export function registerMutationDefaults(client: QueryClient): void {
  client.setMutationDefaults(addItemKey, buildAddItemDefaults(client))
  client.setMutationDefaults(updateItemKey, buildUpdateItemDefaults(client))
  client.setMutationDefaults(checkItemKey, buildCheckDefaults(client, true))
  client.setMutationDefaults(uncheckItemKey, buildCheckDefaults(client, false))
  client.setMutationDefaults(deleteItemKey, buildDeleteItemDefaults(client))
  client.setMutationDefaults(restoreItemKey, buildRestoreItemDefaults(client))
  client.setMutationDefaults(createListKey, buildCreateListDefaults(client))
  client.setMutationDefaults(renameListKey, buildRenameListDefaults(client))
  client.setMutationDefaults(deleteListKey, buildDeleteListDefaults(client))
}

registerMutationDefaults(sharedQueryClient)

// --- Hooks ------------------------------------------------------------------

export function useCreateList() {
  const queryClient = useQueryClient()
  const mutation = useMutation({
    mutationKey: createListKey,
    ...buildCreateListDefaults(queryClient),
  })
  return {
    ...mutation,
    mutate: (name: string) => mutation.mutate({ name, tempId: randomId() }),
  }
}

export function useRenameList(listId: string) {
  const queryClient = useQueryClient()
  const mutation = useMutation({
    mutationKey: renameListKey,
    ...buildRenameListDefaults(queryClient),
  })
  return {
    ...mutation,
    mutate: (name: string) => mutation.mutate({ listId, name }),
  }
}

/** Deletes a list. Call after the undo window elapses (see useUndoableDelete). */
export function useDeleteListMutation() {
  const queryClient = useQueryClient()
  const mutation = useMutation({
    mutationKey: deleteListKey,
    ...buildDeleteListDefaults(queryClient),
  })
  return {
    ...mutation,
    mutate: (listId: string) => mutation.mutate({ listId }),
  }
}

export function useAddItem(listId: string) {
  const queryClient = useQueryClient()
  const mutation = useMutation({ mutationKey: addItemKey, ...buildAddItemDefaults(queryClient) })
  return {
    ...mutation,
    mutate: (body: AddItemRequest) =>
      mutation.mutate({ listId, tempId: randomId(), payload: body }),
  }
}

export function useUpdateItem(listId: string) {
  const queryClient = useQueryClient()
  const mutation = useMutation({
    mutationKey: updateItemKey,
    ...buildUpdateItemDefaults(queryClient),
  })
  return {
    ...mutation,
    mutate: ({ itemId, body }: { itemId: string; body: UpdateItemRequest }) => {
      // Coalesce an edit on a still-PAUSED create into its payload (Key
      // Decision 3): patch the cache only, enqueue no mutation. If no paused
      // create exists (the create is in-flight online, or already succeeded),
      // fall through to a normal update — for an in-flight create it is later
      // remapped from the temp id to the real id on create success.
      if (isTempId(itemId) && updatePausedCreatePayload(queryClient, itemId, body)) {
        queryClient.setQueryData<ListWithItems>(
          listKey(listId),
          (old) =>
            old && {
              ...old,
              items: old.items.map((i) => (i.id === itemId ? { ...i, ...body } : i)),
            },
        )
        return
      }
      mutation.mutate({ listId, itemId, body })
    },
  }
}

export function useCheckItem(listId: string) {
  const queryClient = useQueryClient()
  const mutation = useMutation({
    mutationKey: checkItemKey,
    ...buildCheckDefaults(queryClient, true),
  })
  return {
    ...mutation,
    mutate: (itemId: string) => {
      if (isTempId(itemId) && updatePausedCreatePayload(queryClient, itemId, { checked: true })) {
        toggleChecked(queryClient, listId, itemId, true)
        return
      }
      mutation.mutate({ listId, itemId })
    },
  }
}

export function useUncheckItem(listId: string) {
  const queryClient = useQueryClient()
  const mutation = useMutation({
    mutationKey: uncheckItemKey,
    ...buildCheckDefaults(queryClient, false),
  })
  return {
    ...mutation,
    mutate: (itemId: string) => {
      if (isTempId(itemId) && updatePausedCreatePayload(queryClient, itemId, { checked: false })) {
        toggleChecked(queryClient, listId, itemId, false)
        return
      }
      mutation.mutate({ listId, itemId })
    },
  }
}

/** Deletes an item. Call after the undo window elapses (see useUndoableDelete). */
export function useDeleteItemMutation(listId: string) {
  const queryClient = useQueryClient()
  const mutation = useMutation({
    mutationKey: deleteItemKey,
    ...buildDeleteItemDefaults(queryClient),
  })
  return {
    ...mutation,
    mutate: (itemId: string) => {
      // Deleting a still-PAUSED create cancels it outright (Key Decision 3):
      // remove the paused create from the mutation cache so it never POSTs. The
      // caller has already removed the optimistic item from the cache, so no
      // DELETE is ever sent for an id the server never saw. If no paused create
      // exists (it is in-flight online, or already synced), fall through to a
      // normal delete — for an in-flight create it is remapped from the temp id
      // to the real id on create success, so the DELETE targets the real row.
      if (isTempId(itemId)) {
        const paused = findPausedCreate(queryClient, itemId)
        if (paused) {
          queryClient.getMutationCache().remove(paused)
          return
        }
      }
      mutation.mutate({ listId, itemId })
    },
  }
}

/**
 * Restores a soft-deleted item server-side (US-L.6 undo), optimistically
 * re-inserting it into the cache via `restoreItemLocally`. Unlike list delete,
 * item delete already happened by the time this runs (see
 * `useDeleteItemMutation`) — this mutation is the undo action itself, not a
 * cancellation of a deferred delete.
 */
export function useRestoreItem(listId: string) {
  const queryClient = useQueryClient()
  const mutation = useMutation({
    mutationKey: restoreItemKey,
    ...buildRestoreItemDefaults(queryClient),
  })
  return {
    ...mutation,
    mutate: (item: Item) => mutation.mutate({ listId, item }),
  }
}

/** Optimistically removes an item from the list-detail cache (for the undo window). */
export function removeItemLocally(queryClient: QueryClient, listId: string, item: Item) {
  queryClient.setQueryData<ListWithItems>(
    listKey(listId),
    (old) =>
      old && {
        ...old,
        items: old.items.filter((i) => i.id !== item.id),
        openItemCount: old.openItemCount - (item.checked ? 0 : 1),
        checkedItemCount: old.checkedItemCount - (item.checked ? 1 : 0),
      },
  )
  queryClient.setQueryData<List[]>(listsKey, (old) =>
    old?.map((l) =>
      l.id === listId
        ? {
            ...l,
            openItemCount: l.openItemCount - (item.checked ? 0 : 1),
            checkedItemCount: l.checkedItemCount - (item.checked ? 1 : 0),
          }
        : l,
    ),
  )
}

/** Restores a previously-removed item to the list-detail cache (undo). */
export function restoreItemLocally(queryClient: QueryClient, listId: string, item: Item) {
  queryClient.setQueryData<ListWithItems>(
    listKey(listId),
    (old) =>
      old && {
        ...old,
        items: [...old.items, item],
        openItemCount: old.openItemCount + (item.checked ? 0 : 1),
        checkedItemCount: old.checkedItemCount + (item.checked ? 1 : 0),
      },
  )
  queryClient.setQueryData<List[]>(listsKey, (old) =>
    old?.map((l) =>
      l.id === listId
        ? {
            ...l,
            openItemCount: l.openItemCount + (item.checked ? 0 : 1),
            checkedItemCount: l.checkedItemCount + (item.checked ? 1 : 0),
          }
        : l,
    ),
  )
}

/** Optimistically removes a list from the overview cache (for the undo window). */
export function removeListLocally(queryClient: QueryClient, list: List) {
  queryClient.setQueryData<List[]>(listsKey, (old) => old?.filter((l) => l.id !== list.id))
}

/**
 * Restores a previously-removed list to the overview cache (undo), re-inserting
 * it at its original index when known so ordering survives overlapping deletes.
 */
export function restoreListLocally(queryClient: QueryClient, list: List, index: number) {
  queryClient.setQueryData<List[]>(listsKey, (old) => {
    if (!old) return [list]
    const at = index < 0 || index > old.length ? old.length : index
    return [...old.slice(0, at), list, ...old.slice(at)]
  })
}
