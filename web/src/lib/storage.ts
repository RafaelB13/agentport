// Browser storage fails in private windows and when site data is blocked; the
// panel then works without remembering choices.
export function readStored(key: string): string | null {
  try {
    return window.localStorage.getItem(key)
  } catch {
    return null
  }
}

export function writeStored(key: string, value: string): void {
  try {
    window.localStorage.setItem(key, value)
  } catch {
    // Nothing to do: the choice lasts only for this page.
  }
}
