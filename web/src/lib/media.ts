import { useSyncExternalStore } from 'react'

// useMediaQuery follows a CSS media query, for layout that CSS alone cannot
// switch (such as not rendering a pane at all on narrow screens).
export function useMediaQuery(query: string): boolean {
  return useSyncExternalStore(
    (listener) => {
      const list = window.matchMedia?.(query)
      list?.addEventListener('change', listener)
      return () => list?.removeEventListener('change', listener)
    },
    () => Boolean(window.matchMedia?.(query).matches),
  )
}
