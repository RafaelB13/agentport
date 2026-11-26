import { useEffect, useMemo, useRef, useState } from 'react'
import { ChevronRight, Inbox } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { useDialogs } from '@/lib/dialogs'
import { useI18n } from '@/lib/i18n'
import { sessionNames, usePanel } from '@/lib/panel'
import type { Forward } from '@/lib/types'
import { cn } from '@/lib/utils'
import { EmptyState, ListCard } from './common'
import { ForwardRow } from './forward-row'

type GroupBy = 'group' | 'session' | 'list'

const FRESH_MS = 600

export function OpenPorts() {
  const { t } = useI18n()
  const panel = usePanel()
  const dialogs = useDialogs()
  const [groupBy, setGroupBy] = useState<GroupBy>('group')
  const [session, setSession] = useState('all')
  const [selected, setSelected] = useState<Set<number>>(new Set())
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set())
  const [expanded, setExpanded] = useState<Set<number>>(new Set())
  const [renaming, setRenaming] = useState<number | null>(null)

  // firstSeen marks when each open port appeared, so only new rows fade in;
  // the ports of the first load are already there.
  const firstSeen = useRef<Map<number, number> | null>(null)
  const now = Date.now()
  if (panel.loaded) {
    if (firstSeen.current === null) {
      firstSeen.current = new Map(panel.forwards.map((f) => [f.remote_port, 0]))
    } else {
      for (const f of panel.forwards) if (!firstSeen.current.has(f.remote_port)) firstSeen.current.set(f.remote_port, now)
    }
  }
  const isFresh = (port: number) => now - (firstSeen.current?.get(port) ?? 0) < FRESH_MS

  const names = useMemo(() => {
    const all = new Set(sessionNames(panel.forwards))
    for (const p of panel.pending) if (p.agent) all.add(p.agent)
    return [...all].sort()
  }, [panel.forwards, panel.pending])
  const activeSession = names.includes(session) ? session : 'all'

  const rows = useMemo(
    () => (activeSession === 'all' ? panel.forwards : panel.forwards.filter((f) => (f.owners || []).includes(activeSession))),
    [panel.forwards, activeSession],
  )

  // Forget the selection of ports that closed meanwhile.
  useEffect(() => {
    setSelected((current) => {
      const open = new Set(panel.forwards.map((f) => f.remote_port))
      const next = new Set([...current].filter((port) => open.has(port)))
      return next.size === current.size ? current : next
    })
  }, [panel.forwards])

  const server = panel.status ? panel.status.destination || panel.status.profile : t('this server')

  function groupKey(f: Forward) {
    if (groupBy === 'group') return f.group || t('No group')
    if (groupBy === 'session') return (f.owners || []).filter((o) => o !== 'user').join(', ') || t('You')
    return ''
  }

  function toggle<T>(set: Set<T>, value: T, on?: boolean) {
    const next = new Set(set)
    if (on ?? !next.has(value)) next.add(value)
    else next.delete(value)
    return next
  }

  function confirmClose(remotes: number[], title: string, single?: Forward) {
    const owners = single ? (single.owners || []).filter((o) => o !== 'user') : []
    dialogs.confirm({
      title,
      description: owners.length
        ? t('The agent sessions {owners} will be told that the port is closed.', { owners: owners.join(', ') })
        : t('No agent session is using this port.'),
      actionLabel: t('Close'),
      onConfirm: async () => {
        await panel.closePorts(remotes)
        setSelected(new Set())
      },
    })
  }

  function confirmCloseAll() {
    const n = panel.forwards.length
    dialogs.confirm({
      title: t('Close all {n} ports?', { n }),
      description: t('Every open port on this server closes. Agents are told that the ports are gone.'),
      actionLabel: t('Close all'),
      onConfirm: async () => {
        await panel.closeAll()
        setSelected(new Set())
      },
    })
  }

  function row(f: Forward) {
    return (
      <ForwardRow
        key={f.remote_port}
        forward={f}
        selected={selected.has(f.remote_port)}
        expanded={expanded.has(f.remote_port)}
        renaming={renaming === f.remote_port}
        fresh={isFresh(f.remote_port)}
        onSelect={(on) => setSelected((current) => toggle(current, f.remote_port, on))}
        onToggleDetails={() => {
          const opening = !expanded.has(f.remote_port)
          setExpanded((current) => toggle(current, f.remote_port))
          if (opening) void panel.loadDetected(false)
        }}
        onStartRename={() => setRenaming(f.remote_port)}
        onEndRename={() => setRenaming(null)}
        onEdit={() =>
          dialogs.editPort({ mode: 'edit', remote: f.remote_port, local: f.local_port, label: f.label || '', path: f.path || '' })
        }
        onClose={() => confirmClose([f.remote_port], t('Close :{port}?', { port: f.remote_port }), f)}
      />
    )
  }

  if (!panel.forwards.length) {
    return (
      <ListCard>
        <EmptyState icon={<Inbox />}>
          {panel.offline ? t('The Companion is not answering.') : t('No ports open. The agent asks for a port, or you open one with /.')}
        </EmptyState>
      </ListCard>
    )
  }

  const groups = new Map<string, Forward[]>()
  if (groupBy !== 'list') {
    for (const f of rows) {
      const key = groupKey(f)
      groups.set(key, [...(groups.get(key) || []), f])
    }
  }
  const visiblePorts = rows.map((f) => f.remote_port)
  const allVisibleSelected = visiblePorts.length > 0 && visiblePorts.every((port) => selected.has(port))
  const someVisibleSelected = visiblePorts.some((port) => selected.has(port))

  return (
    <ListCard>
      <div className="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-2 border-b px-3 py-2 sm:px-4">
        <Checkbox
          checked={allVisibleSelected ? true : someVisibleSelected ? 'indeterminate' : false}
          onCheckedChange={(value) =>
            setSelected((current) => {
              const next = new Set(current)
              for (const port of visiblePorts) {
                if (value === true) next.add(port)
                else next.delete(port)
              }
              return next
            })
          }
          aria-label={t('Select all')}
        />
        {names.length > 0 && (
          <Select value={activeSession} onValueChange={setSession}>
            <SelectTrigger className="h-8 w-auto min-w-36 max-w-56 gap-2 text-xs" aria-label={t('Filter by agent session')}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">{t('All sessions')}</SelectItem>
              {names.map((name) => (
                <SelectItem key={name} value={name} className="font-mono text-xs">
                  {name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
        <div className="flex items-center gap-2">
          <span className="hidden text-xs text-muted-foreground sm:inline">{t('Group by')}</span>
          <ToggleGroup
            type="single"
            size="sm"
            value={groupBy}
            onValueChange={(value) => value && setGroupBy(value as GroupBy)}
            aria-label={t('Group by')}
            className="gap-0.5 rounded-md bg-muted p-0.5"
          >
            {(
              [
                ['group', t('Group')],
                ['session', t('Session')],
                ['list', t('List')],
              ] as [GroupBy, string][]
            ).map(([value, label]) => (
              <ToggleGroupItem
                key={value}
                value={value}
                className="h-7 rounded-[5px] px-2.5 text-xs text-muted-foreground hover:bg-transparent hover:text-foreground data-[state=on]:bg-background data-[state=on]:text-foreground data-[state=on]:shadow-sm"
              >
                {label}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        </div>
        <div className="ml-auto flex min-w-0 items-center gap-2">
          {selected.size > 0 ? (
            <>
              <span className="text-xs tabular-nums text-muted-foreground">{t('{n} selected', { n: selected.size })}</span>
              <Button variant="ghost" size="sm" className="h-8" onClick={() => setSelected(new Set())}>
                {t('Clear')}
              </Button>
              <Button
                variant="destructive"
                size="sm"
                className="h-8"
                onClick={() => confirmClose([...selected], t('Close {n} ports?', { n: selected.size }))}
              >
                {t('Close selected')}
              </Button>
            </>
          ) : (
            <Button
              variant="outline"
              size="sm"
              className="h-8 min-w-0 max-w-full text-destructive hover:bg-destructive/10 hover:text-destructive"
              onClick={confirmCloseAll}
            >
              <span className="truncate">{t('Close all on {server}', { server })}</span>
            </Button>
          )}
        </div>
      </div>
      <ScrollArea className="min-h-0 flex-1">
        {!rows.length && <EmptyState icon={<Inbox />}>{t('No port matches this session filter.')}</EmptyState>}
        {groupBy === 'list'
          ? rows.map(row)
          : [...groups].map(([key, items]) => {
              const closedGroup = collapsed.has(key)
              const all = items.every((f) => selected.has(f.remote_port))
              const some = items.some((f) => selected.has(f.remote_port))
              return (
                <section key={key} aria-label={key}>
                  <div className="sticky top-0 z-10 flex items-center gap-3 border-b bg-muted/70 px-3 py-1.5 backdrop-blur sm:px-4">
                    <Checkbox
                      checked={all ? true : some ? 'indeterminate' : false}
                      onCheckedChange={(value) =>
                        setSelected((current) => {
                          const next = new Set(current)
                          for (const f of items) {
                            if (value === true) next.add(f.remote_port)
                            else next.delete(f.remote_port)
                          }
                          return next
                        })
                      }
                      aria-label={t('Select group {name}', { name: key })}
                    />
                    <button
                      type="button"
                      className="flex min-w-0 items-center gap-1.5 rounded-sm text-sm font-semibold focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                      aria-expanded={!closedGroup}
                      onClick={() => setCollapsed((current) => toggle(current, key))}
                    >
                      <ChevronRight
                        className={cn('size-4 shrink-0 text-muted-foreground transition-transform duration-200', !closedGroup && 'rotate-90')}
                        aria-hidden="true"
                      />
                      <span className="truncate">{key}</span>
                    </button>
                    <span className="shrink-0 text-xs text-muted-foreground">
                      {items.length === 1 ? t('1 port') : t('{n} ports', { n: items.length })}
                      {items.some((f) => f.pinned) ? ' · ' + t('pinned') : ''}
                    </span>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="ml-auto h-7 shrink-0 px-2 text-xs text-muted-foreground hover:text-destructive"
                      onClick={() => confirmClose(items.map((f) => f.remote_port), t('Close group {name}?', { name: key }))}
                    >
                      {t('Close group')}
                    </Button>
                  </div>
                  {!closedGroup && items.map(row)}
                </section>
              )
            })}
      </ScrollArea>
    </ListCard>
  )
}
