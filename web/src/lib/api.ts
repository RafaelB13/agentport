// The panel talks only to this Companion. Its base path (/view/<token>) comes
// from the page, where the Companion writes it; the view token in it is the
// only capability the panel needs.

function readBase(): string {
  const value = document.querySelector('meta[name="agentport-base"]')?.getAttribute('content') ?? ''
  // An unfilled placeholder means the page was not served by a Companion (the
  // Vite dev server proxies /api to one instead).
  if (value.includes('{{')) return ''
  return value.replace(/\/+$/, '')
}

export const BASE = readBase()

export class ApiError extends Error {
  status: number
  code?: string

  constructor(message: string, status: number, code?: string) {
    super(message)
    this.status = status
    this.code = code
  }
}

// failureText names an answer without a message; the i18n layer replaces it so
// errors read in the panel's language.
let failureText = (status: number) => `Request failed (${status})`

export function setFailureText(format: (status: number) => string): void {
  failureText = format
}

export async function api<T>(method: string, path: string, body?: unknown): Promise<T> {
  const init: RequestInit = { method, cache: 'no-store', headers: {} }
  if (body !== undefined) {
    init.headers = { 'Content-Type': 'application/json' }
    init.body = JSON.stringify(body)
  }
  const response = await fetch(BASE + path, init)
  let data: unknown = null
  try {
    data = await response.json()
  } catch {
    // Plain-text errors carry no JSON body.
  }
  if (!response.ok) {
    const error = (data ?? {}) as { message?: string; code?: string }
    throw new ApiError(error.message || failureText(response.status), response.status, error.code)
  }
  return data as T
}

export function portsApi<T>(method: string, path: string, body?: unknown): Promise<T> {
  return api<T>(method, '/api/ports' + path, body)
}

export function errorText(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}
