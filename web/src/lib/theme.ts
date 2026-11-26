import { createContext, useContext } from 'react'

export type ThemeChoice = 'system' | 'light' | 'dark'

export interface ThemeState {
  // choice is what the visitor picked; resolved is what the page shows.
  choice: ThemeChoice
  resolved: 'light' | 'dark'
  setChoice: (choice: ThemeChoice) => void
  cycle: () => void
}

export const THEME_ORDER: ThemeChoice[] = ['system', 'light', 'dark']

export const ThemeContext = createContext<ThemeState | null>(null)

export function useTheme(): ThemeState {
  const value = useContext(ThemeContext)
  if (!value) throw new Error('useTheme outside ThemeProvider')
  return value
}
