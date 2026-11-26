import { useEffect, useRef, useState } from 'react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import type { EditOptions } from '@/lib/dialogs'
import { useI18n } from '@/lib/i18n'
import { usePanel } from '@/lib/panel'
import type { LocalCheck } from '@/lib/types'
import { cn } from '@/lib/utils'

const KEEP = 'keep'

type Verdict = { text: string; ok: boolean } | null

export function EditPortDialog({ options, onClose }: { options: EditOptions | null; onClose: () => void }) {
  return (
    <Dialog open={options !== null} onOpenChange={(open) => !open && onClose()}>
      {options && <EditPortForm key={options.mode + options.remote} options={options} onClose={onClose} />}
    </Dialog>
  )
}

function EditPortForm({ options: o, onClose }: { options: EditOptions; onClose: () => void }) {
  const { t } = useI18n()
  const panel = usePanel()
  const [label, setLabel] = useState(o.label)
  const [path, setPath] = useState(o.path)
  const [local, setLocal] = useState(String(o.local))
  const [duration, setDuration] = useState(o.mode === 'edit' ? KEEP : 'session')
  const [verdict, setVerdict] = useState<Verdict>(null)
  const [canSave, setCanSave] = useState(true)
  const run = useRef(0)

  // The local port is checked as it is typed: free, used by an open port of
  // this server, by another profile, or by another program.
  useEffect(() => {
    const id = ++run.current
    const port = Number(local)
    if (!/^\d+$/.test(local.trim()) || !Number.isInteger(port) || port < 1 || port > 65535) {
      setVerdict({ text: t('Use a number between 1 and 65535.'), ok: false })
      setCanSave(false)
      return
    }
    if (o.mode === 'edit' && port === o.local) {
      setVerdict({ text: t('Same port.'), ok: true })
      setCanSave(true)
      return
    }
    const timer = setTimeout(async () => {
      let check: LocalCheck
      try {
        check = await panel.checkLocal(port)
      } catch {
        return
      }
      if (id !== run.current) return
      const messages: Record<string, [string, boolean]> = {
        free: [t('Free.'), true],
        open_port: [t('Already used by an open port of this server.'), false],
        other_profile: [t('Used by another agentport profile.'), false],
        process: [
          t('Used by {process} (pid {pid}).', { process: check.process || t('another program'), pid: check.pid || '—' }),
          false,
        ],
      }
      const [text, ok] = messages[check.status] || [t('Unknown.'), false]
      setVerdict({ text, ok })
      setCanSave(check.status === 'free' || o.mode === 'config')
    }, 200)
    return () => clearTimeout(timer)
    // panel.checkLocal is stable; t changes only with the language.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [local, o.mode, o.local, t])

  const title =
    o.mode === 'reopen'
      ? t('Reopen :{port} with changes', { port: o.remote })
      : o.mode === 'config'
        ? t('Port :{port}', { port: o.remote })
        : t('Edit :{port}', { port: o.remote })

  async function save() {
    const port = Number(local)
    onClose()
    if (o.mode === 'edit') {
      const changes: { label?: string; path?: string; local_port?: number; duration?: string } = {}
      if (label !== o.label) changes.label = label.trim()
      if (path !== o.path) changes.path = path.trim()
      if (port !== o.local) changes.local_port = port
      if (duration !== KEEP) changes.duration = duration
      await panel.editForward(o.remote, changes)
      return
    }
    if (o.mode === 'reopen' && o.closed) {
      await panel.reopen(o.closed, {
        label: label !== o.label ? label.trim() : o.label,
        path: path !== o.path ? path.trim() : o.path,
        local_port: port,
        duration,
      })
      return
    }
    if (o.request) {
      panel.configureRequestPort(o.request.id, o.remote, port)
      toast.success(t('Saved for the approval'))
    }
    await panel.loadState()
  }

  return (
    <DialogContent className="sm:max-w-md">
      <DialogHeader>
        <DialogTitle className="font-mono text-base">{title}</DialogTitle>
        <DialogDescription className="sr-only">{t('Port settings')}</DialogDescription>
      </DialogHeader>
      <form
        className="grid gap-4"
        onSubmit={(event) => {
          event.preventDefault()
          if (canSave) void save()
        }}
      >
        <div className="grid gap-1.5">
          <Label htmlFor="port-name">{t('Name')}</Label>
          <Input id="port-name" value={label} maxLength={64} onChange={(e) => setLabel(e.target.value)} />
        </div>
        <div className="grid gap-1.5">
          <Label htmlFor="port-local">{t('Local port')}</Label>
          <div className="flex items-center gap-2">
            <span className="font-mono text-sm text-muted-foreground">localhost:</span>
            <Input
              id="port-local"
              className="w-28 font-mono"
              inputMode="numeric"
              maxLength={5}
              value={local}
              onChange={(e) => setLocal(e.target.value)}
            />
          </div>
          <p
            role="status"
            className={cn(
              'min-h-5 text-xs',
              verdict?.ok ? 'text-emerald-600 dark:text-emerald-400' : 'text-destructive',
            )}
          >
            {verdict?.text}
          </p>
        </div>
        <div className="grid gap-1.5">
          <Label htmlFor="port-path">{t('Path')}</Label>
          <Input
            id="port-path"
            className="font-mono"
            value={path}
            maxLength={256}
            placeholder="/"
            onChange={(e) => setPath(e.target.value)}
          />
        </div>
        <div className="grid gap-1.5">
          <Label htmlFor="port-duration">{t('Keep the port for')}</Label>
          <Select value={duration} onValueChange={setDuration}>
            <SelectTrigger id="port-duration" aria-label={t('How long to keep the port open')}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {o.mode === 'edit' && <SelectItem value={KEEP}>{t('Keep as it is')}</SelectItem>}
              <SelectItem value="session">{t('This session')}</SelectItem>
              <SelectItem value="1h">{t('1 hour')}</SelectItem>
              <SelectItem value="pin">{t('Until I close it')}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <DialogFooter className="gap-2 sm:gap-0">
          <Button type="button" variant="outline" onClick={onClose}>
            {t('Cancel')}
          </Button>
          <Button type="submit" disabled={!canSave}>
            {o.mode === 'edit' ? t('Save') : t('Open port')}
          </Button>
        </DialogFooter>
      </form>
    </DialogContent>
  )
}
