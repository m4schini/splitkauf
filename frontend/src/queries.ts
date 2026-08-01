// React Query hooks for lists & items. Every mutation applies an optimistic
// update via `onMutate` and rolls back via `onError`, per architecture.md §3,
// so the UI never blocks on a network round-trip for the core add/check loop.
import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
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
  type UpdateItemRequest,
} from './api'

export const listsKey = ['lists'] as const
export const listKey = (listId: string) => ['lists', listId] as const

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

let tempId = 0
function nextTempId(): string {
  tempId += 1
  return `temp-${tempId}`
}

export function useCreateList() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (name: string) => createList({ name }),
    onMutate: async (name: string) => {
      await queryClient.cancelQueries({ queryKey: listsKey })
      const previous = queryClient.getQueryData<List[]>(listsKey)
      const optimistic: List = {
        id: nextTempId(),
        name,
        openItemCount: 0,
        checkedItemCount: 0,
        createdAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(),
      }
      queryClient.setQueryData<List[]>(listsKey, (old) => [...(old ?? []), optimistic])
      return { previous, optimisticId: optimistic.id }
    },
    onError: (_err, _name, context) => {
      if (context) queryClient.setQueryData(listsKey, context.previous)
    },
    onSuccess: (created, _name, context) => {
      queryClient.setQueryData<List[]>(listsKey, (old) =>
        old?.map((l) => (l.id === context?.optimisticId ? created : l)),
      )
    },
  })
}

export function useRenameList(listId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (name: string) => renameList(listId, { name }),
    onMutate: async (name: string) => {
      await queryClient.cancelQueries({ queryKey: listKey(listId) })
      const previousDetail = queryClient.getQueryData<ListWithItems>(listKey(listId))
      const previousLists = queryClient.getQueryData<List[]>(listsKey)
      queryClient.setQueryData<ListWithItems>(listKey(listId), (old) => old && { ...old, name })
      patchListSummary(queryClient, listId, { name })
      return { previousDetail, previousLists }
    },
    onError: (_err, _name, context) => {
      if (context?.previousDetail) queryClient.setQueryData(listKey(listId), context.previousDetail)
      if (context?.previousLists) queryClient.setQueryData(listsKey, context.previousLists)
    },
  })
}

/** Deletes a list. Call after the undo window elapses (see useUndoableDelete). */
export function useDeleteListMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (listId: string) => apiDeleteList(listId),
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: listsKey })
    },
  })
}

export function useAddItem(listId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (body: AddItemRequest) => addItem(listId, body),
    onMutate: async (body: AddItemRequest) => {
      await queryClient.cancelQueries({ queryKey: listKey(listId) })
      const previous = queryClient.getQueryData<ListWithItems>(listKey(listId))
      const optimistic: Item = {
        id: nextTempId(),
        listId,
        name: body.name,
        quantity: body.quantity ?? 1,
        note: body.note ?? null,
        checked: false,
        checkedAt: null,
        createdAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(),
      }
      queryClient.setQueryData<ListWithItems>(
        listKey(listId),
        (old) =>
          old && {
            ...old,
            items: [...old.items, optimistic],
            openItemCount: old.openItemCount + 1,
          },
      )
      patchListSummary(queryClient, listId, {
        openItemCount: (previous?.openItemCount ?? 0) + 1,
      })
      return { previous, optimisticId: optimistic.id }
    },
    onError: (_err, _body, context) => {
      if (context?.previous) {
        queryClient.setQueryData(listKey(listId), context.previous)
        patchListSummary(queryClient, listId, { openItemCount: context.previous.openItemCount })
      }
    },
    onSuccess: (created, _body, context) => {
      queryClient.setQueryData<ListWithItems>(
        listKey(listId),
        (old) =>
          old && {
            ...old,
            items: old.items.map((i) => (i.id === context?.optimisticId ? created : i)),
          },
      )
    },
  })
}

export function useUpdateItem(listId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ itemId, body }: { itemId: string; body: UpdateItemRequest }) =>
      updateItem(listId, itemId, body),
    onMutate: async ({ itemId, body }) => {
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
    onError: (_err, _vars, context) => {
      if (context?.previous) queryClient.setQueryData(listKey(listId), context.previous)
    },
  })
}

function toggleChecked(queryClient: QueryClient, listId: string, itemId: string, checked: boolean) {
  const previous = queryClient.getQueryData<ListWithItems>(listKey(listId))
  queryClient.setQueryData<ListWithItems>(
    listKey(listId),
    (old) =>
      old && {
        ...old,
        items: old.items.map((i) =>
          i.id === itemId
            ? { ...i, checked, checkedAt: checked ? new Date().toISOString() : null }
            : i,
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

export function useCheckItem(listId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (itemId: string) => checkItem(listId, itemId),
    onMutate: async (itemId: string) => {
      await queryClient.cancelQueries({ queryKey: listKey(listId) })
      const previous = toggleChecked(queryClient, listId, itemId, true)
      return { previous }
    },
    onError: (_err, _itemId, context) => {
      if (context?.previous) queryClient.setQueryData(listKey(listId), context.previous)
    },
  })
}

export function useUncheckItem(listId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (itemId: string) => uncheckItem(listId, itemId),
    onMutate: async (itemId: string) => {
      await queryClient.cancelQueries({ queryKey: listKey(listId) })
      const previous = toggleChecked(queryClient, listId, itemId, false)
      return { previous }
    },
    onError: (_err, _itemId, context) => {
      if (context?.previous) queryClient.setQueryData(listKey(listId), context.previous)
    },
  })
}

/** Deletes an item. Call after the undo window elapses (see useUndoableDelete). */
export function useDeleteItemMutation(listId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (itemId: string) => apiDeleteItem(listId, itemId),
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: listKey(listId) })
      queryClient.invalidateQueries({ queryKey: listsKey })
    },
  })
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
  return useMutation({
    mutationFn: (item: Item) => apiRestoreItem(listId, item.id),
    onMutate: async (item: Item) => {
      await queryClient.cancelQueries({ queryKey: listKey(listId) })
      const previous = queryClient.getQueryData<ListWithItems>(listKey(listId))
      restoreItemLocally(queryClient, listId, item)
      return { previous }
    },
    onError: (_err, _item, context) => {
      if (context?.previous) queryClient.setQueryData(listKey(listId), context.previous)
    },
    onSuccess: (restored) => {
      queryClient.setQueryData<ListWithItems>(
        listKey(listId),
        (old) =>
          old && {
            ...old,
            // Upsert rather than a pure replace: a concurrent delete-triggered
            // refetch (see useDeleteItemMutation's onSettled) can land between
            // this mutation's onMutate and onSuccess and drop the item from
            // the cache entirely, so `restored.id` may no longer be present.
            items: old.items.some((i) => i.id === restored.id)
              ? old.items.map((i) => (i.id === restored.id ? restored : i))
              : [...old.items, restored],
          },
      )
    },
    onSettled: () => {
      // Final reconciliation with server truth: the delete mutation's own
      // onSettled refetch can race this one and interleave in either order,
      // so re-invalidate once this restore settles regardless of outcome.
      queryClient.invalidateQueries({ queryKey: listKey(listId) })
    },
  })
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
