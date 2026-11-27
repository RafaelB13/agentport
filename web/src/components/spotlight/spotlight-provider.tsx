import { lazy, Suspense, useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { useDialogs } from '@/lib/dialogs'
import { isTyping } from '@/lib/keys'
import { SpotlightContext, type Tab } from '@/lib/spotlight'

// The search loads as its own chunk, fetched once the panel is idle, so the
// first paint of the panel does not wait for it.
const loadSpotlight = () => import('./spotlight')
const SpotlightDialog = lazy(() => loadSpotlight().then((module) => ({ default: module.SpotlightDialog })))

// SpotlightProvider owns the search: Cmd+K or Ctrl+K toggles it from anywhere,
// / opens it when no field has the focus, and the header and the tabs row open
// it through useSpotlight.
export function SpotlightProvider({
  children,
  onTab,
  openTerminal,
}: {
  children: ReactNode
  onTab: (tab: Tab) => void
  openTerminal: () => void
}) {
  const dialogs = useDialogs()
  // key remounts the search on every opening, so it starts fresh.
  const [state, setState] = useState({ open: false, query: '', key: 0 })

  const open = useCallback((query = '') => setState((s) => ({ open: true, query, key: s.key + 1 })), [])
  const close = useCallback(() => setState((s) => (s.open ? { ...s, open: false } : s)), [])
  const toggle = useCallback(
    () => setState((s) => (s.open ? { ...s, open: false } : { open: true, query: '', key: s.key + 1 })),
    [],
  )

  useEffect(() => {
    const timer = setTimeout(() => void loadSpotlight().catch(() => undefined), 1200)
    return () => clearTimeout(timer)
  }, [])

  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      if (event.defaultPrevented || event.altKey || dialogs.anyOpen) return
      if ((event.metaKey || event.ctrlKey) && !event.shiftKey && event.key.toLowerCase() === 'k') {
        // In the terminal, Ctrl+K belongs to the shell; Cmd+K still searches.
        if (!event.metaKey && event.target instanceof HTMLElement && event.target.closest('.xterm')) return
        event.preventDefault()
        toggle()
        return
      }
      if (event.key === '/' && !event.metaKey && !event.ctrlKey && !isTyping(event.target)) {
        event.preventDefault()
        open()
      }
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [dialogs.anyOpen, open, toggle])

  const value = useMemo(() => ({ isOpen: state.open, open, close, toggle }), [state.open, open, close, toggle])

  return (
    <SpotlightContext.Provider value={value}>
      {children}
      {state.key > 0 && (
        <Suspense fallback={null}>
          <SpotlightDialog
            key={state.key}
            open={state.open}
            initialQuery={state.query}
            onClose={close}
            onTab={onTab}
            openTerminal={openTerminal}
          />
        </Suspense>
      )}
    </SpotlightContext.Provider>
  )
}
