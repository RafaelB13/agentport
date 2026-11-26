import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { readStored, writeStored } from '@/lib/storage'
import { THEME_ORDER, ThemeContext, type ThemeChoice } from '@/lib/theme'

const darkQuery = () => window.matchMedia?.('(prefers-color-scheme: dark)')

function storedChoice(): ThemeChoice {
  const saved = readStored('agentport.theme')
  return saved === 'light' || saved === 'dark' ? saved : 'system'
}

// ThemeProvider applies the auto, light or dark choice to <html>: .dark drives
// Tailwind's dark variants, and .light stops the stylesheet from following the
// system when the visitor chose light.
export function ThemeProvider({ children }: { children: ReactNode }) {
  const [choice, setChoiceState] = useState<ThemeChoice>(storedChoice)
  const [systemDark, setSystemDark] = useState(() => Boolean(darkQuery()?.matches))

  useEffect(() => {
    const query = darkQuery()
    if (!query) return
    const listener = (event: MediaQueryListEvent) => setSystemDark(event.matches)
    query.addEventListener('change', listener)
    return () => query.removeEventListener('change', listener)
  }, [])

  const resolved = choice === 'system' ? (systemDark ? 'dark' : 'light') : choice

  useEffect(() => {
    const root = document.documentElement
    root.classList.toggle('dark', resolved === 'dark')
    root.classList.toggle('light', choice === 'light')
    if (choice === 'system') delete root.dataset.theme
    else root.dataset.theme = choice
  }, [choice, resolved])

  const setChoice = useCallback((next: ThemeChoice) => {
    setChoiceState(next)
    writeStored('agentport.theme', next === 'system' ? '' : next)
  }, [])

  const cycle = useCallback(() => {
    setChoiceState((current) => {
      const next = THEME_ORDER[(THEME_ORDER.indexOf(current) + 1) % THEME_ORDER.length]
      writeStored('agentport.theme', next === 'system' ? '' : next)
      return next
    })
  }, [])

  const value = useMemo(() => ({ choice, resolved, setChoice, cycle }), [choice, resolved, setChoice, cycle])
  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}
