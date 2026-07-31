// RFC 9457 (Problem Details for HTTP APIs) client integration. `apiFetch`
// centralises the single error path: on a problem+json error it throws the
// parsed `ProblemDetail`, so callers handle one typed shape.

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

  return (await res.json()) as T
}
