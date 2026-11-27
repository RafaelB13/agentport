import { useCallback, useMemo, useState, type ReactNode } from 'react'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button, buttonVariants } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'
import { portsApi } from '@/lib/api'
import { DialogsContext, type ConfirmOptions, type EditOptions, type InfoTab } from '@/lib/dialogs'
import { useI18n } from '@/lib/i18n'
import { usePanel } from '@/lib/panel'
import { readStored, writeStored } from '@/lib/storage'
import type { ProfileEntry } from '@/lib/types'
import { EditPortDialog } from './edit-port-dialog'
import { ConnectDialog, InfoDialog, ShortcutsDialog } from './info-dialogs'

const SWITCH_KEY = 'agentport.switch'

export function DialogsProvider({ children }: { children: ReactNode }) {
  const panel = usePanel()
  const [confirmState, setConfirmState] = useState<ConfirmOptions | null>(null)
  const [edit, setEdit] = useState<EditOptions | null>(null)
  const [switching, setSwitching] = useState<ProfileEntry | null>(null)
  const [connect, setConnect] = useState(false)
  const [info, setInfo] = useState<InfoTab | null>(null)
  const [lastInfo, setLastInfo] = useState<InfoTab>('about')
  const [shortcuts, setShortcuts] = useState(false)

  const openCount = panel.forwards.length

  // switchTo opens another running Companion's panel. With ports open here, it
  // asks what happens to them, unless the visitor asked to remember the answer.
  const switchTo = useCallback(
    (profile: ProfileEntry) => {
      if (profile.current || !profile.url) return
      const url = profile.url
      const remembered = readStored(SWITCH_KEY)
      if (openCount > 0 && remembered !== 'keep' && remembered !== 'close') {
        setSwitching(profile)
        return
      }
      if (openCount > 0 && remembered === 'close') {
        portsApi('POST', '/forwards/close-all', {}).finally(() => window.location.assign(url))
        return
      }
      window.location.assign(url)
    },
    [openCount],
  )

  const openInfo = useCallback(
    (tab?: InfoTab) => {
      const next = tab ?? lastInfo
      setLastInfo(next)
      setInfo(next)
    },
    [lastInfo],
  )

  const value = useMemo(
    () => ({
      confirm: setConfirmState,
      editPort: setEdit,
      switchTo,
      openConnect: () => setConnect(true),
      openInfo,
      openShortcuts: () => setShortcuts(true),
      anyOpen: Boolean(confirmState || edit || switching || connect || info || shortcuts),
    }),
    [switchTo, openInfo, confirmState, edit, switching, connect, info, shortcuts],
  )

  return (
    <DialogsContext.Provider value={value}>
      {children}
      <ConfirmDialog options={confirmState} onClose={() => setConfirmState(null)} />
      <EditPortDialog options={edit} onClose={() => setEdit(null)} />
      <SwitchDialog profile={switching} openCount={openCount} onClose={() => setSwitching(null)} />
      <ConnectDialog open={connect} onClose={() => setConnect(false)} />
      <InfoDialog
        tab={info}
        onTab={(tab) => {
          setLastInfo(tab)
          setInfo(tab)
        }}
        onClose={() => setInfo(null)}
      />
      <ShortcutsDialog open={shortcuts} onClose={() => setShortcuts(false)} />
    </DialogsContext.Provider>
  )
}

function ConfirmDialog({ options, onClose }: { options: ConfirmOptions | null; onClose: () => void }) {
  const { t } = useI18n()
  return (
    <AlertDialog open={options !== null} onOpenChange={(open) => !open && onClose()}>
      <AlertDialogContent className="sm:max-w-md">
        <AlertDialogHeader>
          <AlertDialogTitle>{options?.title}</AlertDialogTitle>
          <AlertDialogDescription>{options?.description}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
          <AlertDialogAction
            className={buttonVariants({ variant: 'destructive' })}
            onClick={() => {
              const action = options?.onConfirm
              onClose()
              void action?.()
            }}
          >
            {options?.actionLabel}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

function SwitchDialog({
  profile,
  openCount,
  onClose,
}: {
  profile: ProfileEntry | null
  openCount: number
  onClose: () => void
}) {
  const { t } = useI18n()
  const [remember, setRemember] = useState(false)
  const server = profile ? profile.destination || profile.name : ''

  function go(choice: 'keep' | 'close') {
    const url = profile?.url
    onClose()
    if (!url) return
    if (remember) writeStored(SWITCH_KEY, choice)
    if (choice === 'close') {
      // Switch even when closing fails: the other panel still opens.
      portsApi('POST', '/forwards/close-all', {}).finally(() => window.location.assign(url))
      return
    }
    window.location.assign(url)
  }

  return (
    <AlertDialog open={profile !== null} onOpenChange={(open) => !open && onClose()}>
      <AlertDialogContent className="sm:max-w-lg">
        <AlertDialogHeader>
          <AlertDialogTitle>{t('Switch to {server}?', { server })}</AlertDialogTitle>
          <AlertDialogDescription>
            {t('{n} ports are open on this server. What happens to them?', { n: openCount })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <div className="flex items-center gap-2">
          <Checkbox id="switch-remember" checked={remember} onCheckedChange={(value) => setRemember(value === true)} />
          <Label htmlFor="switch-remember" className="font-medium">
            {t('Remember my choice')}
          </Label>
        </div>
        <AlertDialogFooter className="gap-2 sm:gap-0">
          <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
          <Button variant="outline" onClick={() => go('keep')}>
            {t('Keep open and switch')}
          </Button>
          <Button variant="destructive" onClick={() => go('close')}>
            {t('Close {n} and switch', { n: openCount })}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
