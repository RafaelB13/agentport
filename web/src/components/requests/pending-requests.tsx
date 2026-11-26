import { useEffect, useState } from 'react'
import { TriangleAlert } from 'lucide-react'
import { PortRoute } from '@/components/ports/common'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { useDialogs } from '@/lib/dialogs'
import { useI18n } from '@/lib/i18n'
import { usePanel, type ConflictChoice } from '@/lib/panel'
import { isTyping } from '@/lib/keys'
import type { Duration, LocalCheck, PendingRequest } from '@/lib/types'
import { cn } from '@/lib/utils'

type Conflicts = Record<string, Record<number, ConflictChoice>>

const BATCH_VISIBLE = 10

// requestWho names the agent and folder that asked, showing only what is known:
// "agent" and a bare "/" are placeholders sent when the client said no more.
function useRequestWho() {
  const { t } = useI18n()
  return (req: PendingRequest) => {
    const agent = req.agent && req.agent !== 'agent' ? req.agent : ''
    const cwd = req.cwd && req.cwd !== '/' ? req.cwd : ''
    return [agent || t('an agent'), cwd].filter(Boolean).join(' · ')
  }
}

export function PendingRequests() {
  const { t } = useI18n()
  const panel = usePanel()
  const dialogs = useDialogs()
  const [conflicts, setConflicts] = useState<Conflicts>({})

  // A approves the oldest request (every port, for this session); R denies it.
  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      if (isTyping(event.target) || event.ctrlKey || event.metaKey || event.altKey || dialogs.anyOpen) return
      const req = panel.pending[0]
      if (!req) return
      if (event.key === 'a' || event.key === 'A') {
        void panel.approve(req, new Set(req.ports.map((p) => p.remote_port)), 'session', conflicts[req.id])
      } else if (event.key === 'r' || event.key === 'R') {
        void panel.deny(req)
      }
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [panel, dialogs.anyOpen, conflicts])

  if (!panel.pending.length) return null

  return (
    <section aria-labelledby="requests-title" className="shrink-0">
      <h2 id="requests-title" className="mb-2 flex items-center gap-2 text-sm font-semibold">
        {t('Requests from the agent')}
        <Badge variant="warning" className="px-1.5 tabular-nums">
          {panel.pending.length}
        </Badge>
      </h2>
      <div className="grid max-h-[40vh] gap-2 overflow-y-auto">
        {panel.pending.map((req) =>
          req.ports.length > 1 ? (
            <BatchRequest key={req.id} req={req} />
          ) : (
            <SingleRequest
              key={req.id}
              req={req}
              conflicts={conflicts[req.id] || {}}
              onConflict={(port, choice) =>
                setConflicts((current) => ({ ...current, [req.id]: { ...current[req.id], [port]: choice } }))
              }
            />
          ),
        )}
      </div>
    </section>
  )
}

function DurationSelect({ value, onChange }: { value: Duration; onChange: (value: Duration) => void }) {
  const { t } = useI18n()
  return (
    <Select value={value} onValueChange={(next) => onChange(next as Duration)}>
      <SelectTrigger className="h-8 w-auto min-w-36 gap-2 text-xs" aria-label={t('How long to keep the port open')}>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="session">{t('This session')}</SelectItem>
        <SelectItem value="1h">{t('1 hour')}</SelectItem>
        <SelectItem value="pin">{t('Until I close it')}</SelectItem>
      </SelectContent>
    </Select>
  )
}

const cardClass =
  'border-amber-500/40 bg-amber-500/[0.04] p-3 shadow-sm motion-safe:animate-in motion-safe:fade-in-0 motion-safe:slide-in-from-top-1 sm:p-4'

function SingleRequest({
  req,
  conflicts,
  onConflict,
}: {
  req: PendingRequest
  conflicts: Record<number, ConflictChoice>
  onConflict: (port: number, choice: ConflictChoice) => void
}) {
  const { t } = useI18n()
  const panel = usePanel()
  const dialogs = useDialogs()
  const who = useRequestWho()
  const [duration, setDuration] = useState<Duration>('session')
  const port = req.ports[0]
  const settings = () =>
    dialogs.editPort({
      mode: 'config',
      remote: port.remote_port,
      local: panel.requestPortLocal(req.id, port.remote_port) ?? port.remote_port,
      label: '',
      path: '',
      request: req,
    })

  return (
    <Card className={cardClass}>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <PortRoute remote={port.remote_port} local={port.remote_port} />
        <span className="min-w-0 truncate text-xs text-muted-foreground">{who(req)}</span>
      </div>
      {req.reason && <p className="mt-1.5 text-sm">{req.reason}</p>}
      {(port.label || port.path) && (
        <p className="mt-1 truncate font-mono text-xs text-muted-foreground">{[port.label, port.path].filter(Boolean).join(' · ')}</p>
      )}
      <ConflictBox
        remote={port.remote_port}
        choice={conflicts[port.remote_port]}
        onChoice={(choice) => onConflict(port.remote_port, choice)}
        onSettings={settings}
      />
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <Button
          size="sm"
          className="h-8"
          onClick={() => void panel.approve(req, new Set([port.remote_port]), duration, conflicts)}
        >
          {t('Approve')}
        </Button>
        <Button size="sm" variant="outline" className="h-8" onClick={() => void panel.deny(req)}>
          {t('Deny')}
        </Button>
        <DurationSelect value={duration} onChange={setDuration} />
        <Button size="sm" variant="link" className="h-8 px-1 text-xs" onClick={settings}>
          {t('Port settings')}
        </Button>
      </div>
    </Card>
  )
}

// ConflictBox shows what holds the local port of a requested port, and lets the
// visitor use another free port or stop that program when agentport can.
function ConflictBox({
  remote,
  choice,
  onChoice,
  onSettings,
}: {
  remote: number
  choice: ConflictChoice | undefined
  onChoice: (choice: ConflictChoice) => void
  onSettings: () => void
}) {
  const { t } = useI18n()
  const { checkLocal } = usePanel()
  const [check, setCheck] = useState<LocalCheck | null>(null)

  useEffect(() => {
    let current = true
    checkLocal(remote)
      .then((result) => current && setCheck(result))
      .catch(() => undefined)
    return () => {
      current = false
    }
  }, [remote, checkLocal])

  if (!check || check.status === 'free' || check.status === 'open_port') return null
  const holder = check.process ? t('{process} (pid {pid})', { process: check.process, pid: check.pid ?? '—' }) : t('another program')
  const message =
    check.status === 'other_profile'
      ? t('Port {port} is held by another agentport profile.', { port: remote })
      : t('Port {port} is busy: {who}.', { port: remote, who: holder })

  return (
    <div className="mt-3 space-y-2 rounded-md border border-amber-500/40 bg-background/60 p-3 text-sm">
      <p className="flex items-start gap-2">
        <TriangleAlert className="mt-0.5 size-4 shrink-0 text-amber-600 dark:text-amber-400" aria-hidden="true" />
        <span>
          <span className="font-semibold">{t('Busy port')}</span> {message}
        </span>
      </p>
      <RadioGroup value={choice ?? 'remap'} onValueChange={(value) => onChoice(value as ConflictChoice)} className="gap-1.5 pl-6">
        <div className="flex items-center gap-2">
          <RadioGroupItem value="remap" id={`remap-${remote}`} />
          <Label htmlFor={`remap-${remote}`} className="font-normal">
            {t('Use another free port')}
          </Label>
        </div>
        <div className="flex items-center gap-2">
          <RadioGroupItem value="stop" id={`stop-${remote}`} disabled={!check.stoppable} />
          <Label htmlFor={`stop-${remote}`} className={cn('font-normal', !check.stoppable && 'opacity-50')}>
            {t('Stop {process} and use :{port}', { process: check.process || t('it'), port: remote })}
          </Label>
        </div>
      </RadioGroup>
      {!check.stoppable && <p className="pl-6 text-xs text-muted-foreground">{t('This program is not one agentport can stop.')}</p>}
      <Button variant="link" size="sm" className="h-auto p-0 pl-6 text-xs" onClick={onSettings}>
        {t('Port settings')}
      </Button>
    </div>
  )
}

function BatchRequest({ req }: { req: PendingRequest }) {
  const { t } = useI18n()
  const panel = usePanel()
  const who = useRequestWho()
  const [selected, setSelected] = useState<Set<number>>(() => new Set(req.ports.map((p) => p.remote_port)))
  const [duration, setDuration] = useState<Duration>('session')
  const [showAll, setShowAll] = useState(false)
  const total = req.ports.length
  const visible = showAll ? req.ports : req.ports.slice(0, BATCH_VISIBLE)

  return (
    <Card className={cardClass}>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <span className="text-sm font-semibold">{t('{n} ports', { n: total })}</span>
        <span className="min-w-0 truncate text-xs text-muted-foreground">{who(req)}</span>
      </div>
      {req.reason && <p className="mt-1.5 text-sm">{req.reason}</p>}
      <div className="mt-3 overflow-hidden rounded-md border bg-background/60">
        <div className="flex items-center gap-2 border-b px-3 py-2 text-xs">
          <Checkbox
            id={`all-${req.id}`}
            checked={selected.size === total ? true : selected.size ? 'indeterminate' : false}
            onCheckedChange={(value) => setSelected(value === true ? new Set(req.ports.map((p) => p.remote_port)) : new Set())}
          />
          <Label htmlFor={`all-${req.id}`} className="text-xs font-medium">
            {t('Select all')}
          </Label>
          <span className="ml-auto tabular-nums text-muted-foreground">{t('{n} selected', { n: selected.size })}</span>
        </div>
        <div className="max-h-64 overflow-y-auto">
          {visible.map((p) => {
            const on = selected.has(p.remote_port)
            return (
              <label
                key={p.remote_port}
                className={cn(
                  'grid cursor-pointer grid-cols-[auto_10rem_minmax(0,1fr)] items-center gap-3 border-b px-3 py-1.5 text-sm last:border-b-0 hover:bg-muted/40',
                  !on && 'text-muted-foreground',
                )}
              >
                <Checkbox
                  checked={on}
                  onCheckedChange={(value) =>
                    setSelected((current) => {
                      const next = new Set(current)
                      if (value === true) next.add(p.remote_port)
                      else next.delete(p.remote_port)
                      return next
                    })
                  }
                  aria-label={t('Select :{port}', { port: p.remote_port })}
                />
                <PortRoute remote={p.remote_port} local={p.remote_port} className={cn(!on && 'opacity-60')} />
                <span className="min-w-0 truncate">
                  <span className="font-medium">{p.label || ''}</span>
                  {p.path && <span className="ml-2 font-mono text-xs text-muted-foreground">{p.path}</span>}
                </span>
              </label>
            )
          })}
        </div>
        {total > BATCH_VISIBLE && (
          <Button variant="ghost" size="sm" className="h-8 w-full rounded-none border-t text-xs" onClick={() => setShowAll(!showAll)}>
            {showAll ? t('Show less') : t('Show {n} more', { n: total - BATCH_VISIBLE })}
          </Button>
        )}
      </div>
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <Button size="sm" className="h-8" disabled={selected.size === 0} onClick={() => void panel.approve(req, selected, duration)}>
          {t('Approve {n}', { n: selected.size })}
        </Button>
        <Button size="sm" variant="outline" className="h-8" onClick={() => void panel.deny(req)}>
          {t('Deny all')}
        </Button>
        <DurationSelect value={duration} onChange={setDuration} />
      </div>
    </Card>
  )
}
