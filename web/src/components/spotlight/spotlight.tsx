import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent } from 'react'
import * as DialogPrimitive from '@radix-ui/react-dialog'
import { Command as CommandPrimitive } from 'cmdk'
import { ChevronLeft, LoaderCircle, Plus, Search, TextCursorInput, TriangleAlert } from 'lucide-react'
import { useI18n, type Translate } from '@/lib/i18n'
import { useMediaQuery } from '@/lib/media'
import { usePanel } from '@/lib/panel'
import {
  MOD_KEY,
  parseQuery,
  readRecent,
  rememberRecent,
  RECENT_SHOWN,
  scoreItem,
  scoreText,
  validPort,
  type GroupId,
  type ParsedQuery,
  type SpotAction,
  type SpotItem,
  type Tab,
} from '@/lib/spotlight'
import { useStopArming } from '@/lib/stop'
import { useTheme } from '@/lib/theme'
import type { Forward } from '@/lib/types'
import { cn } from '@/lib/utils'
import { Hint, Kbd, Keys, Tile } from './parts'
import { SpotlightPreview } from './preview'
import { useSpotlightItems } from './use-items'

type View = { kind: 'list' } | { kind: 'actions'; itemId: string } | { kind: 'rename'; forward: Forward }

interface Section {
  id: GroupId | 'actions' | 'rename'
  heading: string
  items: SpotItem[]
}

// The groups in their usual order; with a query, the group with the best
// match comes first.
const ORDER: GroupId[] = ['requests', 'open', 'det', 'closed', 'servers', 'commands', 'activity']
const PORT_GROUPS = new Set<GroupId>(['requests', 'open', 'det', 'closed'])

function headingOf(t: Translate, id: Section['id']): string {
  switch (id) {
    case 'top':
      return t('Top hit')
    case 'recent':
      return t('Recent')
    case 'requests':
      return t('Requests from the agent')
    case 'open':
      return t('Open ports')
    case 'det':
      return t('Detected on server')
    case 'closed':
      return t('Closed')
    case 'servers':
      return t('Servers on this computer')
    case 'commands':
      return t('Commands')
    case 'activity':
      return t('Activity')
    case 'suggest':
      return t('Suggestions')
    default:
      return ''
  }
}

// resolveRecent turns remembered ids into today's items. A port remembered
// while detected may be open now, and the other way round.
function resolveRecent(ids: string[], byId: Map<string, SpotItem>): SpotItem[] {
  const out: SpotItem[] = []
  for (const id of ids) {
    let item = byId.get(id)
    const port = /^(?:fwd|det):(\d+)$/.exec(id)?.[1]
    if (!item && port) item = byId.get('fwd:' + port) || byId.get('det:' + port)
    if (item && !item.disabled && !out.includes(item)) out.push(item)
    if (out.length === RECENT_SHOWN) break
  }
  return out
}

function buildSections(
  t: Translate,
  items: SpotItem[],
  parsed: ParsedQuery,
  recent: string[],
  suggestions: string[],
  typed: SpotItem | null,
): Section[] {
  const pool = items.filter((item) =>
    parsed.mode === 'commands' ? item.group === 'commands' : parsed.mode === 'ports' ? PORT_GROUPS.has(item.group) : true,
  )
  const section = (id: Section['id'], list: SpotItem[]): Section => ({ id, heading: headingOf(t, id), items: list })

  // Nothing typed: what was used last, what waits, what is open, and ideas.
  if (!parsed.text && parsed.mode === 'all') {
    const byId = new Map(items.map((item) => [item.id, item]))
    const recentItems = resolveRecent(recent, byId)
    const used = new Set(recentItems.map((item) => item.id))
    const fresh = (item: SpotItem | undefined): item is SpotItem => Boolean(item) && !used.has(item!.id)
    return [
      section('recent', recentItems),
      section('requests', pool.filter((item) => item.group === 'requests' && fresh(item))),
      section('open', pool.filter((item) => item.group === 'open' && fresh(item))),
      section('suggest', suggestions.map((id) => byId.get(id)).filter(fresh)),
    ].filter((s) => s.items.length)
  }

  // Only a prefix: everything of that kind, in the usual order.
  if (!parsed.text) {
    return ORDER.map((id) => section(id, pool.filter((item) => item.group === id))).filter((s) => s.items.length)
  }

  const scored = pool
    .filter((item) => item.group !== 'activity' || parsed.mode === 'all')
    .map((item) => ({ item, score: scoreItem(item, parsed) }))
    .filter((entry) => entry.score > 0)
  if (typed) scored.push({ item: typed, score: typed.disabled ? 4500 : 4000 })
  scored.sort((a, b) => b.score - a.score)
  if (!scored.length) return []

  const limit = (id: GroupId) => (parsed.mode !== 'all' ? 60 : id === 'activity' ? 4 : 6)
  const groups = new Map<GroupId, { best: number; items: SpotItem[] }>()
  for (const { item, score } of scored.slice(1)) {
    const group = groups.get(item.group) || { best: score, items: [] }
    if (group.items.length < limit(item.group)) group.items.push(item)
    groups.set(item.group, group)
  }
  const ordered = [...groups].sort(
    ([a, ga], [b, gb]) => gb.best - ga.best || ORDER.indexOf(a) - ORDER.indexOf(b),
  )
  return [section('top', [scored[0].item]), ...ordered.map(([id, group]) => section(id, group.items))]
}

const valueOf = (section: Section['id'], id: string) => section + '|' + id

// caretAt tells whether the caret of the search field sits at its start or end,
// so the arrows move through actions only when they would not move the caret.
function caretAt(input: HTMLInputElement | null, where: 'start' | 'end'): boolean {
  if (!input) return true
  const { selectionStart, selectionEnd, value } = input
  if (selectionStart !== selectionEnd) return false
  return where === 'start' ? selectionStart === 0 : selectionStart === value.length
}

// Highlight is the tinted bar under the selected row. It slides from row to
// row: it follows cmdk's aria-selected through a MutationObserver and moves
// with a transform, so it costs no React render.
function Highlight() {
  const ref = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => {
    const bar = ref.current
    const list = bar?.closest('[cmdk-list]')
    if (!bar || !list) return
    let shown = false
    const place = () => {
      const row = list.querySelector<HTMLElement>('[cmdk-item][aria-selected="true"]')
      if (!row) {
        bar.style.opacity = '0'
        shown = false
        return
      }
      if (!shown) bar.style.transition = 'none'
      bar.style.transform = `translateY(${row.offsetTop}px)`
      bar.style.height = `${row.offsetHeight}px`
      bar.style.opacity = '1'
      if (!shown) {
        void bar.offsetHeight
        bar.style.transition = ''
        shown = true
      }
    }
    place()
    const observer = new MutationObserver(place)
    observer.observe(list, { subtree: true, childList: true, attributes: true, attributeFilter: ['aria-selected'] })
    return () => observer.disconnect()
  }, [])
  return (
    <div
      ref={ref}
      aria-hidden="true"
      className="pointer-events-none absolute inset-x-0 top-0 rounded-[10px] bg-brand/[0.13] opacity-0 shadow-[inset_0_0_0_1px_hsl(var(--brand)/0.28)] transition-[transform,height] duration-150 ease-out dark:bg-brand/[0.2]"
    />
  )
}

function Row({ item, value, selected, onSelect }: { item: SpotItem; value: string; selected: boolean; onSelect: () => void }) {
  return (
    <CommandPrimitive.Item
      value={value}
      disabled={item.disabled}
      onSelect={onSelect}
      className="relative flex h-12 cursor-default select-none items-center gap-3 rounded-[10px] px-2 outline-none data-[disabled=true]:opacity-55"
    >
      <Tile icon={item.icon} tone={item.tone} />
      <span className="flex min-w-0 flex-1 flex-col">
        <span className={cn('truncate text-sm font-semibold leading-5', item.danger && 'text-destructive')}>{item.title}</span>
        {item.subtitle && (
          <span
            className={cn(
              'truncate leading-4 text-muted-foreground',
              item.monoSubtitle ? 'font-mono text-[11.5px]' : 'text-[12.5px] font-medium',
            )}
          >
            {item.subtitle}
          </span>
        )}
      </span>
      {item.meta && <span className="max-w-[9rem] shrink-0 truncate font-mono text-xs font-medium text-muted-foreground">{item.meta}</span>}
      {item.keys && <Keys keys={item.keys} className="shrink-0" />}
      {selected && !item.disabled && item.actions.length > 0 && <Kbd className="shrink-0 max-sm:hidden">↵</Kbd>}
    </CommandPrimitive.Item>
  )
}

function ActionRow({ action, value, index, onSelect }: { action: SpotAction; value: string; index: number; onSelect: () => void }) {
  const Icon = action.icon
  return (
    <CommandPrimitive.Item
      value={value}
      onSelect={onSelect}
      className="relative flex h-10 cursor-default select-none items-center gap-3 rounded-[10px] px-2.5 outline-none"
    >
      <Icon className={cn('size-4 shrink-0', action.destructive ? 'text-destructive' : 'text-muted-foreground')} aria-hidden="true" />
      <span className={cn('min-w-0 flex-1 truncate text-sm font-semibold', action.destructive && 'text-destructive')}>{action.label}</span>
      {index === 0 && <Keys keys={['↵']} />}
      {index === 1 && <Keys keys={[MOD_KEY, '↵']} />}
    </CommandPrimitive.Item>
  )
}

export interface SpotlightDialogProps {
  open: boolean
  initialQuery: string
  onClose: () => void
  onTab: (tab: Tab) => void
  openTerminal: () => void
}

// SpotlightDialog is the search: a translucent panel near the top of the page
// that finds ports, requests, servers, commands and activity, ranks them as you
// type, and runs them from the keyboard.
export function SpotlightDialog({ open, initialQuery, onClose, onTab, openTerminal }: SpotlightDialogProps) {
  const { t } = useI18n()
  const panel = usePanel()
  const { resolved } = useTheme()
  const stop = useStopArming()
  const wide = useMediaQuery('(min-width: 900px)')
  const roomy = useMediaQuery('(min-width: 640px)')
  const [query, setQueryState] = useState(initialQuery)
  const [view, setView] = useState<View>({ kind: 'list' })
  const [value, setValue] = useState('')
  const [recent, setRecent] = useState<string[]>(readRecent)
  const inputRef = useRef<HTMLInputElement>(null)
  const restoreFocus = useRef(true)
  // Where the list was before the actions or the name field took its place.
  const saved = useRef({ query: '', value: '' })

  const close = onClose
  const setQuery = useCallback((next: string) => setQueryState(next), [])

  const startRename = useCallback(
    (forward: Forward) => {
      setView({ kind: 'rename', forward })
      setQueryState(forward.label || '')
    },
    [],
  )

  const handlers = useMemo(
    () => ({ onTab, openTerminal, startRename, setQuery, close, stop }),
    [onTab, openTerminal, startRename, setQuery, close, stop],
  )
  const items = useSpotlightItems(handlers)

  // A fresh scan when the search opens, as the port picker did, and the servers.
  const { loadDetected, loadProfiles } = panel
  useEffect(() => {
    if (!open) return
    void loadDetected(true)
    void loadProfiles()
  }, [open, loadDetected, loadProfiles])

  const parsed = useMemo(() => parseQuery(query), [query])

  // typed is the "Open port N" row: a number no open or detected port has.
  const typed = useMemo<SpotItem | null>(() => {
    if (view.kind !== 'list' || parsed.port === null) return null
    const n = parsed.port
    if (!validPort(n)) {
      return {
        id: 'new:invalid',
        group: 'open',
        title: t('Use a number between 1 and 65535.'),
        icon: TriangleAlert,
        tone: 'red',
        fields: [],
        disabled: true,
        actions: [],
        data: { kind: 'new', port: parsed.port, valid: false },
      }
    }
    if (panel.forwards.some((f) => f.remote_port === n) || panel.detected.some((p) => p.port === n)) return null
    return {
      id: 'new:' + n,
      group: 'open',
      title: t('Open port :{port}', { port: n }),
      subtitle: t('Forward this port of the server to this computer.'),
      icon: Plus,
      tone: 'brand',
      meta: ':' + n,
      fields: [],
      port: n,
      actions: [{ id: 'open', label: t('Open port'), icon: Plus, run: () => panel.openPort(n) }],
      data: { kind: 'new', port: n, valid: true },
    }
  }, [view.kind, parsed.port, panel, t])

  const suggestions = useMemo(
    () => ['cmd:browse', 'cmd:tab:det', 'cmd:terminal', 'cmd:theme:' + (resolved === 'dark' ? 'light' : 'dark'), 'cmd:shortcuts', 'cmd:help:about'],
    [resolved],
  )

  const actionItem = view.kind === 'actions' ? items.find((item) => item.id === view.itemId) || null : null

  const sections = useMemo<Section[]>(() => {
    if (view.kind === 'rename') {
      const f = view.forward
      const name = query.trim()
      return [
        {
          id: 'rename',
          heading: t('Rename :{port}', { port: f.remote_port }),
          items: [
            {
              id: 'rename',
              group: 'open',
              title: name || t('No name'),
              subtitle: f.url + (f.path || ''),
              monoSubtitle: true,
              icon: TextCursorInput,
              tone: 'brand',
              fields: [],
              actions: [{ id: 'save', label: t('Save'), icon: TextCursorInput, run: () => panel.rename(f, name) }],
              data: { kind: 'port', forward: f, info: panel.portInfo.get(f.remote_port) },
            },
          ],
        },
      ]
    }
    if (view.kind === 'actions') return []
    return buildSections(t, items, parsed, recent, suggestions, typed)
  }, [view, query, items, parsed, recent, suggestions, typed, panel, t])

  const actionRows = useMemo(() => {
    if (!actionItem) return []
    const words = query.trim()
    return actionItem.actions
      .map((action, index) => ({ action, index, score: words ? scoreText(words, action.label) : 1 }))
      .filter((row) => row.score > 0)
  }, [actionItem, query])

  // Every selectable value, in order, with what it stands for.
  const entries = useMemo(() => {
    const map = new Map<string, { item: SpotItem; action?: SpotAction }>()
    if (actionItem) {
      for (const row of actionRows) map.set(valueOf('actions', row.action.id), { item: actionItem, action: row.action })
    } else {
      for (const s of sections) for (const item of s.items) map.set(valueOf(s.id, item.id), { item })
    }
    return map
  }, [sections, actionItem, actionRows])

  // cmdk selects the first row itself after typing and when the selected row
  // goes away.
  const onValueChange = useCallback((next: string) => setValue(next), [])

  // A selection that is no longer listed falls back to the first row.
  useEffect(() => {
    if (!value || entries.has(value) || !entries.size) return
    const first = [...entries].find(([, entry]) => !entry.item.disabled) || [...entries][0]
    setValue(first[0])
  }, [entries, value])

  const current = entries.get(value) || null
  const selected = current?.item || null

  // A port that closed while its actions were shown leaves the actions.
  useEffect(() => {
    if (view.kind === 'actions' && !actionItem) setView({ kind: 'list' })
  }, [view.kind, actionItem])

  function remember(item: SpotItem, action: SpotAction) {
    if (item.group === 'activity' || item.disabled || item.id === 'rename') return
    if (action.keepOpen && action.id !== 'rename') return
    rememberRecent(item.id.startsWith('new:') ? 'fwd:' + item.port : item.id)
    setRecent(readRecent())
  }

  function run(item: SpotItem, action: SpotAction | undefined) {
    if (!action || item.disabled) return
    remember(item, action)
    if (action.keepOpen) {
      void action.run()
      return
    }
    restoreFocus.current = !action.away
    close()
    void Promise.resolve(action.run()).catch(() => undefined)
  }

  function openActions(item: SpotItem) {
    if (!item.actions.length || item.disabled) return
    saved.current = { query, value }
    setView({ kind: 'actions', itemId: item.id })
    setQueryState('')
  }

  // back returns from the actions or the name field to the list as it was:
  // the same query and, once cmdk has picked its first row for that query, the
  // same row.
  function back() {
    const { query: savedQuery, value: savedValue } = saved.current
    setView({ kind: 'list' })
    setQueryState(savedQuery)
    requestAnimationFrame(() => requestAnimationFrame(() => setValue(savedValue)))
  }

  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.nativeEvent.isComposing) return
    const input = inputRef.current
    if (event.key === 'Enter') {
      // cmdk would run onSelect too; the search decides here, with Cmd/Ctrl.
      event.preventDefault()
      if (!current) return
      if (view.kind === 'list') run(current.item, current.item.actions[event.metaKey || event.ctrlKey ? 1 : 0])
      else if (view.kind === 'rename') run(current.item, current.item.actions[0])
      else run(current.item, current.action)
      return
    }
    if (event.key === 'Tab') {
      event.preventDefault()
      if (view.kind === 'list' && !event.shiftKey && selected) openActions(selected)
      else if (view.kind !== 'list' && event.shiftKey) back()
      return
    }
    if (view.kind === 'list' && event.key === 'ArrowRight' && selected && caretAt(input, 'end')) {
      event.preventDefault()
      openActions(selected)
      return
    }
    if (view.kind === 'actions' && (event.key === 'ArrowLeft' || event.key === 'Backspace') && !query && caretAt(input, 'start')) {
      event.preventDefault()
      back()
    }
  }

  const primary = selected && !selected.disabled ? (view.kind === 'actions' ? current?.action : selected.actions[0]) : undefined
  const secondary = view.kind === 'list' && selected && !selected.disabled ? selected.actions[1] : undefined
  const placeholder =
    view.kind === 'actions'
      ? t('Search actions…')
      : view.kind === 'rename'
        ? t('New name for :{port}', { port: view.forward.remote_port })
        : roomy
          ? t('Search ports, servers and commands…')
          : t('Search…')
  const showHints = view.kind === 'list' && !query
  const empty = view.kind === 'actions' ? !actionRows.length : !sections.length
  const portsNote = view.kind === 'list' && parsed.mode === 'ports' && !parsed.text && !panel.detected.length

  return (
    <DialogPrimitive.Root open={open} onOpenChange={(next) => !next && close()}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-zinc-950/15 backdrop-blur-[2px] duration-150 data-[state=closed]:animate-out data-[state=open]:animate-in data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 dark:bg-black/30" />
        <DialogPrimitive.Content
          aria-describedby={undefined}
          onOpenAutoFocus={(event) => {
            event.preventDefault()
            const input = inputRef.current
            input?.focus()
            input?.setSelectionRange(input.value.length, input.value.length)
          }}
          onCloseAutoFocus={(event) => {
            if (!restoreFocus.current) event.preventDefault()
          }}
          onEscapeKeyDown={(event) => {
            if (view.kind !== 'list') {
              event.preventDefault()
              back()
            }
          }}
          className={cn(
            'fixed inset-x-0 top-[8vh] z-50 mx-auto w-[640px] max-w-[92vw] overflow-hidden rounded-2xl sm:top-[18vh]',
            'bg-popover/80 text-popover-foreground backdrop-blur-xl backdrop-saturate-150',
            'shadow-[0_32px_80px_-16px_rgba(0,0,0,0.45),0_12px_28px_-10px_rgba(0,0,0,0.3)] ring-1 ring-black/10 dark:ring-white/[0.08]',
            'before:pointer-events-none before:absolute before:inset-0 before:rounded-2xl before:shadow-[inset_0_1px_0_rgba(255,255,255,0.5)] dark:before:shadow-[inset_0_1px_0_rgba(255,255,255,0.07)]',
            '[animation-duration:160ms] ease-out data-[state=closed]:animate-out data-[state=open]:animate-in data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-[0.96] data-[state=open]:zoom-in-[0.96]',
            'min-[900px]:w-[760px]',
          )}
        >
          <DialogPrimitive.Title className="sr-only">{t('Search')}</DialogPrimitive.Title>
          <CommandPrimitive
            label={t('Search')}
            shouldFilter={false}
            loop
            vimBindings={false}
            value={value}
            onValueChange={onValueChange}
            onKeyDown={onKeyDown}
            className="flex flex-col outline-none [&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:pb-1.5 [&_[cmdk-group-heading]]:pt-3 [&_[cmdk-group-heading]]:text-[11px] [&_[cmdk-group-heading]]:font-semibold [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-[0.06em] [&_[cmdk-group-heading]]:text-muted-foreground [&_[cmdk-list-sizer]]:relative"
          >
            <div className="flex h-14 items-center gap-3 px-4">
              {view.kind === 'list' ? (
                <Search className="size-[22px] shrink-0 text-muted-foreground" strokeWidth={2.25} aria-hidden="true" />
              ) : (
                <button
                  type="button"
                  tabIndex={-1}
                  onClick={back}
                  aria-label={t('Back')}
                  className="flex h-7 min-w-0 max-w-[45%] shrink-0 items-center gap-1 rounded-md bg-foreground/[0.07] pl-1 pr-2 text-xs font-semibold text-foreground transition-colors hover:bg-foreground/[0.12]"
                >
                  <ChevronLeft className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
                  <span className="truncate">{view.kind === 'actions' ? actionItem?.title : ':' + view.forward.remote_port}</span>
                </button>
              )}
              <CommandPrimitive.Input
                ref={inputRef}
                value={query}
                onValueChange={setQueryState}
                placeholder={placeholder}
                className="h-full min-w-0 flex-1 bg-transparent text-xl font-medium tracking-[-0.01em] text-foreground outline-none placeholder:text-muted-foreground/70"
              />
              {panel.detectedBusy && view.kind === 'list' && parsed.mode !== 'commands' && (
                <LoaderCircle className="size-4 shrink-0 text-muted-foreground motion-safe:animate-spin" aria-hidden="true" />
              )}
              {view.kind === 'list' && parsed.mode !== 'all' && (
                <span className="shrink-0 rounded-md bg-brand/15 px-2 py-0.5 text-xs font-semibold text-brand-strong">
                  {parsed.mode === 'commands' ? t('Commands') : t('Ports')}
                </span>
              )}
              {showHints && (
                <span className="hidden shrink-0 items-center gap-3 text-xs font-medium text-muted-foreground sm:flex">
                  <button type="button" tabIndex={-1} className="flex items-center gap-1.5 hover:text-foreground" onClick={() => setQueryState('>')}>
                    <Kbd>&gt;</Kbd>
                    {t('Commands')}
                  </button>
                  <button type="button" tabIndex={-1} className="flex items-center gap-1.5 hover:text-foreground" onClick={() => setQueryState(':')}>
                    <Kbd>:</Kbd>
                    {t('Ports')}
                  </button>
                </span>
              )}
            </div>

            <div className="grid border-t border-black/[0.07] dark:border-white/[0.07] min-[900px]:grid-cols-[minmax(0,1fr)_18.5rem]">
              <CommandPrimitive.List
                className="max-h-[min(34rem,calc(100dvh-8vh-8.5rem))] overflow-y-auto overflow-x-hidden overscroll-contain scroll-py-2 px-2 pb-2 sm:max-h-[min(26rem,calc(100dvh-18vh-8.5rem))] min-[900px]:h-[min(26rem,calc(100dvh-18vh-8.5rem))]"
                aria-label={t('Results')}
              >
                <Highlight />
                {view.kind === 'actions' && actionItem && actionRows.length > 0 && (
                  <CommandPrimitive.Group heading={t('Actions')}>
                    {actionRows.map(({ action, index }) => (
                      <ActionRow
                        key={action.id}
                        action={action}
                        index={index}
                        value={valueOf('actions', action.id)}
                        onSelect={() => run(actionItem, action)}
                      />
                    ))}
                  </CommandPrimitive.Group>
                )}
                {view.kind !== 'actions' &&
                  sections.map((s) => (
                    <CommandPrimitive.Group key={s.id} heading={s.heading}>
                      {s.items.map((item) => {
                        const v = valueOf(s.id, item.id)
                        return (
                          <Row
                            key={v}
                            item={item}
                            value={v}
                            selected={v === value}
                            onSelect={() => run(item, item.actions[0])}
                          />
                        )
                      })}
                    </CommandPrimitive.Group>
                  ))}
                {portsNote && (
                  <p className="px-2 py-3 text-xs font-medium text-muted-foreground">
                    {panel.detectedBusy ? t('Scanning the server…') : t('No other listening ports were found on the server.')}
                  </p>
                )}
                {empty && (
                  <div className="flex flex-col items-center gap-1.5 px-6 py-14 text-center">
                    <Search className="mb-1 size-6 text-muted-foreground/60" aria-hidden="true" />
                    <p className="text-sm font-semibold">{t('No results for “{query}”', { query: query.trim() })}</p>
                    <p className="text-xs font-medium text-muted-foreground">{t('Try a port number, a process name or a command.')}</p>
                  </div>
                )}
              </CommandPrimitive.List>
              {wide && (
                <aside
                  className="hidden min-h-0 border-l border-black/[0.07] bg-foreground/[0.015] dark:border-white/[0.07] min-[900px]:block min-[900px]:h-[min(26rem,calc(100dvh-18vh-8.5rem))]"
                  aria-label={t('Details')}
                >
                  <SpotlightPreview
                    item={actionItem || selected}
                    showActions={view.kind === 'list'}
                    onRun={(action) => selected && run(selected, action)}
                    onMore={() => selected && openActions(selected)}
                  />
                </aside>
              )}
            </div>

            <div className="flex h-10 items-center gap-4 border-t border-black/[0.07] bg-foreground/[0.025] px-4 text-xs font-medium text-muted-foreground dark:border-white/[0.07]">
              <Hint keys={['↑', '↓']} label={t('Navigate')} className="max-sm:hidden" />
              {primary && <Hint keys={['↵']} label={primary.label} />}
              {secondary && <Hint keys={[MOD_KEY, '↵']} label={secondary.label} className="max-md:hidden" />}
              {view.kind === 'list' && selected && !selected.disabled && selected.actions.length > 1 && (
                <button type="button" tabIndex={-1} className="hover:text-foreground" onClick={() => openActions(selected)}>
                  <Hint keys={['→']} label={t('Actions')} />
                </button>
              )}
              <span className="ml-auto flex items-center gap-1.5">
                <Kbd>esc</Kbd>
                {view.kind === 'list' ? t('Close') : t('Back')}
              </span>
            </div>
          </CommandPrimitive>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  )
}
