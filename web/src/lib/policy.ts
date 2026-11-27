import { useCallback, useSyncExternalStore } from 'react'
import { toast } from 'sonner'
import { useI18n } from './i18n'
import { readStored, writeStored } from './storage'

export type Policy = 'ask' | 'session'

const KEY = 'agentport.policy'
const listeners = new Set<() => void>()
let current: Policy = readStored(KEY) === 'session' ? 'session' : 'ask'

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

// usePolicy is the approval policy, a choice of this browser as it always was.
// The header menu and the search share it, so a change in one shows in both.
export function usePolicy() {
  const { t } = useI18n()
  const policy = useSyncExternalStore(subscribe, () => current)
  const setPolicy = useCallback(
    (next: Policy) => {
      current = next
      writeStored(KEY, next)
      listeners.forEach((listener) => listener())
      toast(
        next === 'session'
          ? t('Ports you approve reopen without asking until the session ends.')
          : t('Each port asks for approval.'),
      )
    },
    [t],
  )
  return { policy, setPolicy }
}
