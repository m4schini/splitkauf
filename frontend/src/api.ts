// RFC 9457 (Problem Details for HTTP APIs) client integration. `apiFetch`
// centralises the single error path: on a problem+json error it throws the
// parsed `ProblemDetail`, so callers handle one typed shape.

import { queryClient, persister } from './queryClient'

/** A field-level validation error, per the RFC 9457 `errors` extension. */
export interface FieldError {
  detail: string
  pointer?: string
}

/**
 * An RFC 9457 Problem Details object. All standard members are optional; the
 * index signature accommodates extension members beyond `errors`.
 */
export interface ProblemDetail {
  type?: string
  title?: string
  status?: number
  detail?: string
  instance?: string
  errors?: FieldError[]
  [key: string]: unknown
}

const problemContentType = 'application/problem+json'

/**
 * Fetch `input` and return the parsed JSON body typed as `T`.
 *
 * On a non-2xx response the body is parsed as a `ProblemDetail` when the
 * response is `application/problem+json` and thrown; otherwise a minimal
 * `ProblemDetail` built from the HTTP status/statusText is thrown. Either way,
 * callers `catch` a `ProblemDetail`.
 */
export async function apiFetch<T>(input: RequestInfo | URL, init?: RequestInit): Promise<T> {
  const res = await fetch(input, init)

  if (!res.ok) {
    const contentType = res.headers.get('Content-Type') ?? ''
    if (contentType.includes(problemContentType)) {
      throw (await res.json()) as ProblemDetail
    }
    throw { status: res.status, title: res.statusText } satisfies ProblemDetail
  }

  if (res.status === 204) {
    return undefined as T
  }

  return (await res.json()) as T
}

const unauthorizedProblemType = '/problems/unauthorized'

/**
 * True when `err` is a `ProblemDetail` signalling that the request was
 * unauthenticated (HTTP 401). Callers use this to distinguish "not signed
 * in" from other failures (network errors, 404s, ...) without `apiFetch`
 * itself performing any hard redirect — the caller decides how to react.
 */
export function isUnauthorized(err: unknown): boolean {
  if (typeof err !== 'object' || err === null) return false
  const problem = err as ProblemDetail
  return problem.status === 401 || problem.type === unauthorizedProblemType
}

/** The server's active authentication mode, from GET /api/auth/config. */
export type AuthMode = 'oidc' | 'password' | 'dev'

/**
 * GET /api/auth/config — the active auth mode, so the UI can render the right
 * login affordance (a username/password form vs the OIDC redirect button). The
 * endpoint is public and returns only the mode.
 */
export function getAuthConfig(): Promise<{ mode: AuthMode }> {
  return apiFetch<{ mode: AuthMode }>('/api/auth/config')
}

/**
 * Starts the BFF login flow via a top-level navigation (not `fetch`) so the
 * browser follows the server's 302 to the IdP. `returnTo` defaults to the
 * current path+query so the user lands back where they were after login.
 */
export function login(returnTo?: string): void {
  const target = returnTo ?? window.location.pathname + window.location.search
  window.location.href = '/api/auth/login?return_to=' + encodeURIComponent(target)
}

/**
 * POST /api/auth/login — submit username/password credentials (password auth
 * mode). On success the server sets the HttpOnly session cookie and returns
 * 204; on failure `apiFetch` throws the RFC 9457 `ProblemDetail` (a 401 for a
 * wrong username or password — the two are indistinguishable by design).
 */
export function passwordLogin(username: string, password: string): Promise<void> {
  return apiFetch<void>('/api/auth/login', {
    method: 'POST',
    headers: jsonHeaders,
    body: JSON.stringify({ username, password }),
  })
}

/**
 * Logs out via a top-level form POST (not `fetch`/XHR) to `/api/auth/logout`,
 * so the browser follows the server's redirect and the session cookie is
 * cleared. An XHR POST would not follow a cross-origin redirect to the IdP's
 * RP-initiated logout endpoint the way a real navigation does.
 *
 * Before navigating away, the persisted React Query cache is cleared (both
 * in-memory and in IndexedDB) so a shared device never renders the next
 * signed-in member's lists from a stale, still-persisted cache (US-O.2 Key
 * Decision 1). The IndexedDB removal is awaited before the navigation is
 * kicked off — an unawaited `del` transaction can be aborted by the page
 * unload, leaving the previous member's cache behind on a shared device.
 */
export async function logout(): Promise<void> {
  queryClient.clear()
  await persister.removeClient()

  const form = document.createElement('form')
  form.method = 'POST'
  form.action = '/api/auth/logout'
  form.style.display = 'none'
  document.body.appendChild(form)
  form.submit()
}

// --- Domain types (mirrors splitkauf.openapi.yaml) ------------------------

/** The authenticated user (US-A.1). `email` is populated in OIDC mode. */
export interface User {
  id: string
  name: string
  email?: string
}

/**
 * The user credited with an action (US-L.11). Only the id is stored server-side;
 * `name` is resolved when the resource is read, so a profile rename applies to
 * every past action. It is null when no member record matches the id, in which
 * case there is nothing worth displaying — except to that user themselves, who
 * is recognised by id (see `attributionLabel`).
 */
export interface Attribution {
  id: string
  name: string | null
}

/** A shopping list with a summary of its item counts. */
export interface List {
  id: string
  name: string
  openItemCount: number
  checkedItemCount: number
  /** Who created the list; absent for lists that predate attribution. */
  createdBy?: Attribution
  createdAt: string
  updatedAt: string
}

/**
 * A grocery unit token (mirrors the `Unit` enum in splitkauf.openapi.yaml and
 * `lists.Units()`). `amount` is the default and is rendered bare (just the
 * number) in item rows; the rest carry a short German label (see `unitLabels`).
 */
export type Unit =
  'amount' | 'g' | 'kg' | 'ml' | 'l' | 'pack' | 'bottle' | 'can' | 'jar' | 'cup' | 'bunch' | 'bag'

/** The canonical unit tokens in display order (for building the selector). */
export const units: Unit[] = [
  'amount',
  'g',
  'kg',
  'ml',
  'l',
  'pack',
  'bottle',
  'can',
  'jar',
  'cup',
  'bunch',
  'bag',
]

/**
 * German display labels for each unit. `amount` maps to "Stück", but item rows
 * render `amount` bare (just the quantity) — the label is used for the unit
 * selector's option text. Singular labels are used for every quantity; no
 * pluralization (US-L.9 keeps this deliberately simple).
 */
export const unitLabels: Record<Unit, string> = {
  amount: 'Stück',
  g: 'g',
  kg: 'kg',
  ml: 'ml',
  l: 'l',
  pack: 'Packung',
  bottle: 'Flasche',
  can: 'Dose',
  jar: 'Glas',
  cup: 'Becher',
  bunch: 'Bund',
  bag: 'Beutel',
}

/** A single item on a shopping list. */
export interface Item {
  id: string
  listId: string
  name: string
  quantity: number
  unit: Unit
  note?: string | null
  checked: boolean
  checkedAt?: string | null
  /** Who added the item; absent for items that predate attribution. */
  addedBy?: Attribution
  /** Who checked it off. Absent while the item is open — uncheck clears it. */
  boughtBy?: Attribution | null
  createdAt: string
  updatedAt: string
}

/**
 * How to address the user credited with an action, or null when there is
 * nothing worth showing. The viewer is always "you" — recognised by id, so it
 * works even for an account whose name never resolved. Anyone else is shown by
 * name; a nameless stranger yields null and the caller renders no line at all,
 * rather than exposing a raw UUID.
 */
export function attributionLabel(
  attribution: Attribution | null | undefined,
  meId: string | undefined,
): string | null {
  if (!attribution) return null
  if (meId && attribution.id === meId) return 'you'
  return attribution.name || null
}

/** A list together with all of its items. */
export interface ListWithItems extends List {
  items: Item[]
}

export interface CreateListRequest {
  name: string
}

export interface RenameListRequest {
  name: string
}

/**
 * Optional body for a list copy. Omitting `name` (or the body entirely) lets
 * the server name the copy "«Original name» (copy)".
 */
export interface CopyListRequest {
  name?: string
}

export interface AddItemRequest {
  name: string
  quantity?: number
  unit?: Unit
  note?: string | null
  checked?: boolean
}

export interface UpdateItemRequest {
  name?: string
  quantity?: number
  unit?: Unit
  note?: string | null
}

// --- Endpoint helpers -------------------------------------------------------

const jsonHeaders = { 'Content-Type': 'application/json' }

/** GET /me — the authenticated (dev) user (US-A.1). */
export function getMe(): Promise<User> {
  return apiFetch<User>('/api/v1/me')
}

/** GET /lists — every shopping list with item-count summaries (US-L.2). */
export function listLists(): Promise<List[]> {
  return apiFetch<List[]>('/api/v1/lists')
}

/** POST /lists — create a new shopping list (US-L.1). */
export function createList(body: CreateListRequest): Promise<List> {
  return apiFetch<List>('/api/v1/lists', {
    method: 'POST',
    headers: jsonHeaders,
    body: JSON.stringify(body),
  })
}

/** GET /lists/{listId} — a list with all of its items (US-L.2). */
export function getList(listId: string): Promise<ListWithItems> {
  return apiFetch<ListWithItems>(`/api/v1/lists/${listId}`)
}

/** PATCH /lists/{listId} — rename a list (US-L.3). */
export function renameList(listId: string, body: RenameListRequest): Promise<List> {
  return apiFetch<List>(`/api/v1/lists/${listId}`, {
    method: 'PATCH',
    headers: jsonHeaders,
    body: JSON.stringify(body),
  })
}

/** DELETE /lists/{listId} — delete a list and all of its items (US-L.3). */
export function deleteList(listId: string): Promise<void> {
  return apiFetch<void>(`/api/v1/lists/${listId}`, { method: 'DELETE' })
}

/**
 * POST /lists/{listId}/copy — copy a list, every item unchecked (US-L.10).
 *
 * The body is optional and is sent only when a name is supplied: the request
 * validator rejects a non-empty body that lacks the JSON content type, and a
 * body-less request is exactly how the server is told to derive the name.
 */
export function copyList(listId: string, body?: CopyListRequest): Promise<List> {
  return apiFetch<List>(`/api/v1/lists/${listId}/copy`, {
    method: 'POST',
    ...(body ? { headers: jsonHeaders, body: JSON.stringify(body) } : {}),
  })
}

/** POST /lists/{listId}/items — add an item to a list (US-L.4). */
export function addItem(listId: string, body: AddItemRequest): Promise<Item> {
  return apiFetch<Item>(`/api/v1/lists/${listId}/items`, {
    method: 'POST',
    headers: jsonHeaders,
    body: JSON.stringify(body),
  })
}

/** PATCH /lists/{listId}/items/{itemId} — update an item (US-L.5). */
export function updateItem(listId: string, itemId: string, body: UpdateItemRequest): Promise<Item> {
  return apiFetch<Item>(`/api/v1/lists/${listId}/items/${itemId}`, {
    method: 'PATCH',
    headers: jsonHeaders,
    body: JSON.stringify(body),
  })
}

/** DELETE /lists/{listId}/items/{itemId} — remove an item (US-L.6). */
export function deleteItem(listId: string, itemId: string): Promise<void> {
  return apiFetch<void>(`/api/v1/lists/${listId}/items/${itemId}`, { method: 'DELETE' })
}

/** POST /lists/{listId}/items/{itemId}/check — check off an item (US-L.7). */
export function checkItem(listId: string, itemId: string): Promise<Item> {
  return apiFetch<Item>(`/api/v1/lists/${listId}/items/${itemId}/check`, { method: 'POST' })
}

/** POST /lists/{listId}/items/{itemId}/uncheck — return an item to the open list (US-L.8). */
export function uncheckItem(listId: string, itemId: string): Promise<Item> {
  return apiFetch<Item>(`/api/v1/lists/${listId}/items/${itemId}/uncheck`, { method: 'POST' })
}

/**
 * POST /lists/{listId}/items/{itemId}/restore — restore a soft-deleted item
 * (US-L.6 undo). Idempotent: restoring an item that isn't deleted is a no-op.
 */
export function restoreItem(listId: string, itemId: string): Promise<Item> {
  return apiFetch<Item>(`/api/v1/lists/${listId}/items/${itemId}/restore`, { method: 'POST' })
}
