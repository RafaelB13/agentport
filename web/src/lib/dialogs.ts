import { createContext, useContext } from 'react'
import type { ClosedForward, PendingRequest, ProfileEntry } from './types'

export interface ConfirmOptions {
  title: string
  description: string
  actionLabel: string
  onConfirm: () => void | Promise<void>
}

// EditOptions opens the port dialog: edit an open forward, reopen a closed one
// with changes, or set the local port of a requested port before approving it.
export interface EditOptions {
  mode: 'edit' | 'reopen' | 'config'
  remote: number
  local: number
  label: string
  path: string
  closed?: ClosedForward
  request?: PendingRequest
}

export type InfoTab = 'about' | 'start' | 'mcp' | 'sec' | 'trouble'

export interface Dialogs {
  confirm: (options: ConfirmOptions) => void
  editPort: (options: EditOptions) => void
  switchTo: (profile: ProfileEntry) => void
  openConnect: () => void
  openInfo: (tab?: InfoTab) => void
  openShortcuts: () => void
  // anyOpen is true while a dialog is on screen; keyboard shortcuts wait.
  anyOpen: boolean
}

export const DialogsContext = createContext<Dialogs | null>(null)

export function useDialogs(): Dialogs {
  const value = useContext(DialogsContext)
  if (!value) throw new Error('useDialogs outside DialogsProvider')
  return value
}
