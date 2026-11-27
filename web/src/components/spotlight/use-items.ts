import { useMemo } from 'react'
import {
  Activity,
  ArrowLeftRight,
  Ban,
  Bell,
  BellOff,
  BellRing,
  Bug,
  Check,
  Copy,
  ExternalLink,
  History,
  Info,
  Keyboard,
  Languages,
  Lock,
  Pencil,
  Pin,
  PinOff,
  Play,
  Plug,
  Plus,
  Power,
  Radar,
  RefreshCw,
  RotateCcw,
  ScanSearch,
  Server,
  ShieldCheck,
  SlidersHorizontal,
  SquareTerminal,
  Sun,
  Moon,
  SunMoon,
  TextCursorInput,
  Trash2,
  TriangleAlert,
  PanelTopOpen,
  X,
  type LucideIcon,
} from 'lucide-react'
import { toast } from 'sonner'
import { useDialogs } from '@/lib/dialogs'
import { closedReason, fmtDateTime, fmtTime, remoteProcessName } from '@/lib/format'
import { LANG_NAMES, useI18n, type Translate } from '@/lib/i18n'
import { LANGS } from '@/lib/language.js'
import { ISSUES_URL, PROJECT_LABEL, PROJECT_URL } from '@/lib/links'
import { usePanel } from '@/lib/panel'
import { usePolicy } from '@/lib/policy'
import type { SpotAction, SpotItem, Tab, Tone } from '@/lib/spotlight'
import { themeLabel, useTheme, type ThemeChoice } from '@/lib/theme'
import type { Forward, PendingRequest } from '@/lib/types'

export interface ItemHandlers {
  onTab: (tab: Tab) => void
  openTerminal: () => void
  // startRename turns the search into a name field for an open port.
  startRename: (forward: Forward) => void
  // setQuery types into the search (the "browse ports" suggestion uses it).
  setQuery: (query: string) => void
  close: () => void
  stop: { armed: boolean; press: () => boolean }
}

export function openInNewTab(href: string) {
  window.open(href, '_blank', 'noopener,noreferrer')
}

async function copyText(t: Translate, text: string) {
  try {
    await navigator.clipboard.writeText(text)
    toast.success(t('Copied'))
  } catch {
    toast.error(t('Could not copy'))
  }
}

const ACTIVITY_TONE: Record<string, Tone> = { ok: 'emerald', ask: 'amber', no: 'red', info: 'sky' }

// requestWho names the agent and folder that asked, like the request cards.
function requestWho(t: Translate, req: PendingRequest): string {
  const agent = req.agent && req.agent !== 'agent' ? req.agent : ''
  const cwd = req.cwd && req.cwd !== '/' ? req.cwd : ''
  return [agent || t('an agent'), cwd].filter(Boolean).join(' · ')
}

// useSpotlightItems lists everything the search can find: the ports in every
// state, the agent's requests, the servers, the panel's commands and the
// activity. Each item carries what Enter, Cmd+Enter and its actions list do.
export function useSpotlightItems(handlers: ItemHandlers): SpotItem[] {
  const { t, lang, setLang } = useI18n()
  const panel = usePanel()
  const dialogs = useDialogs()
  const theme = useTheme()
  const { policy, setPolicy } = usePolicy()
  const { onTab, openTerminal, startRename, setQuery, close, stop } = handlers

  return useMemo(() => {
    const items: SpotItem[] = []
    const server = panel.status ? panel.status.destination || panel.status.profile : t('this server')

    // Open ports: Enter opens the address in a new tab.
    for (const f of panel.forwards) {
      const port = f.remote_port
      const info = panel.portInfo.get(port)
      const process = remoteProcessName(info)
      const name = f.label || process || t('No name')
      const href = f.url + (f.path || '')
      const owners = (f.owners || []).filter((o) => o !== 'user')
      const actions: SpotAction[] = [
        { id: 'open', label: t('Open in a new tab'), icon: ExternalLink, href, run: () => openInNewTab(href) },
        { id: 'copy', label: t('Copy URL'), icon: Copy, run: () => copyText(t, href) },
        { id: 'pin', label: f.pinned ? t('Unpin') : t('Pin'), icon: f.pinned ? PinOff : Pin, run: () => panel.togglePin(f) },
        { id: 'rename', label: t('Rename :{port}', { port }), icon: TextCursorInput, keepOpen: true, run: () => startRename(f) },
        {
          id: 'edit',
          label: t('Edit'),
          icon: Pencil,
          away: true,
          run: () => dialogs.editPort({ mode: 'edit', remote: port, local: f.local_port, label: f.label || '', path: f.path || '' }),
        },
        {
          id: 'close',
          label: t('Close'),
          icon: X,
          destructive: true,
          away: true,
          run: () =>
            dialogs.confirm({
              title: t('Close :{port}?', { port }),
              description: owners.length
                ? t('The agent sessions {owners} will be told that the port is closed.', { owners: owners.join(', ') })
                : t('No agent session is using this port.'),
              actionLabel: t('Close'),
              onConfirm: () => panel.closePorts([port]),
            }),
        },
      ]
      items.push({
        id: 'fwd:' + port,
        group: 'open',
        title: name,
        subtitle: href,
        monoSubtitle: true,
        icon: ArrowLeftRight,
        tone: f.state === 'active' ? 'emerald' : 'brand',
        meta: ':' + port,
        fields: [name, ':' + port, href, 'localhost:' + f.local_port, ...owners, f.group || '', process, info?.cmdline || '', f.path || ''],
        port,
        actions,
        data: { kind: 'port', forward: f, info },
      })
    }

    // Requests from the agent: Enter approves every port for this session,
    // like the A key; Cmd+Enter denies, like R.
    for (const req of panel.pending) {
      const single = req.ports.length === 1 ? req.ports[0] : null
      const title = single ? ':' + single.remote_port + (single.label ? ' · ' + single.label : '') : t('{n} ports', { n: req.ports.length })
      const actions: SpotAction[] = [
        {
          id: 'approve',
          label: single ? t('Approve') : t('Approve {n}', { n: req.ports.length }),
          icon: Check,
          run: () => panel.approve(req, new Set(req.ports.map((p) => p.remote_port)), 'session'),
        },
        { id: 'deny', label: single ? t('Deny') : t('Deny all'), icon: Ban, destructive: true, run: () => panel.deny(req) },
      ]
      if (single) {
        actions.push({
          id: 'settings',
          label: t('Port settings'),
          icon: SlidersHorizontal,
          away: true,
          run: () =>
            dialogs.editPort({
              mode: 'config',
              remote: single.remote_port,
              local: panel.requestPortLocal(req.id, single.remote_port) ?? single.remote_port,
              label: '',
              path: '',
              request: req,
            }),
        })
      }
      items.push({
        id: 'req:' + req.id,
        group: 'requests',
        title,
        subtitle: req.reason || requestWho(t, req),
        icon: BellRing,
        tone: 'amber',
        meta: requestWho(t, req).split(' · ')[0],
        fields: [title, req.agent, req.cwd || '', req.reason, ...req.ports.map((p) => ':' + p.remote_port + ' ' + (p.label || ''))],
        port: single?.remote_port,
        actions,
        data: { kind: 'request', request: req },
      })
    }

    // Detected on the server: Enter forwards the port, like "Open port" in
    // the Detected tab.
    for (const p of panel.detected) {
      const name = p.process || t('Unknown process')
      const actions: SpotAction[] = [
        {
          id: 'open',
          label: t('Open port'),
          icon: Plus,
          run: async () => {
            await panel.openPort(p.port)
            await panel.loadDetected(true)
          },
        },
      ]
      if (p.cmdline) actions.push({ id: 'copy', label: t('Copy command line'), icon: Copy, run: () => copyText(t, p.cmdline || '') })
      actions.push({ id: 'tab', label: t('Show ports detected on the server'), icon: Radar, run: () => onTab('det') })
      items.push({
        id: 'det:' + p.port,
        group: 'det',
        title: name,
        subtitle: p.cmdline || [p.bind, p.user].filter(Boolean).join(' · '),
        monoSubtitle: true,
        icon: Radar,
        tone: 'sky',
        meta: ':' + p.port,
        fields: [name, ':' + p.port, p.cmdline || '', p.user || '', p.bind, p.cwd || ''],
        port: p.port,
        actions,
        data: { kind: 'detected', port: p },
      })
    }

    // Closed ports, newest first: Enter reopens with the same settings.
    ;[...panel.closed].reverse().forEach((c, index) => {
      const name = c.label || remoteProcessName(panel.portInfo.get(c.remote_port)) || t('No name')
      const reason = closedReason(t, c.reason)
      items.push({
        id: 'closed:' + c.remote_port + ':' + c.closed_at,
        group: 'closed',
        title: name,
        subtitle: reason + ' · ' + fmtDateTime(lang, c.closed_at),
        icon: History,
        tone: 'zinc',
        meta: ':' + c.remote_port,
        fields: [name, ':' + c.remote_port, 'localhost:' + c.local_port, c.path || '', reason],
        port: c.remote_port,
        rank: index,
        actions: [
          { id: 'reopen', label: t('Reopen'), icon: RotateCcw, run: () => panel.reopen(c) },
          {
            id: 'changes',
            label: t('Reopen with changes'),
            icon: SlidersHorizontal,
            away: true,
            run: () =>
              dialogs.editPort({
                mode: 'reopen',
                remote: c.remote_port,
                local: c.local_port,
                label: c.label || '',
                path: c.path || '',
                closed: c,
              }),
          },
          { id: 'forget', label: t('Remove from history'), icon: Trash2, destructive: true, run: () => panel.forgetClosed(c) },
        ],
        data: { kind: 'closed', closed: c },
      })
    })

    // Servers on this computer: Enter switches to another running Companion.
    for (const p of panel.profiles) {
      const count = p.forwards ?? 0
      const target = p.destination || p.name
      const counter = p.current ? t('Current') : p.running ? (count === 1 ? t('1 port') : t('{n} ports', { n: count })) : t('stopped')
      const actions: SpotAction[] = p.current
        ? [{ id: 'show', label: t('Show open ports'), icon: ArrowLeftRight, run: () => onTab('open') }]
        : [{ id: 'switch', label: t('Switch to this server'), icon: Server, away: true, run: () => dialogs.switchTo(p) }]
      if (p.running && count > 0 && !p.current) {
        actions.push({
          id: 'close',
          label: t('Close the ports of {server}', { server: target }),
          icon: X,
          destructive: true,
          away: true,
          run: () =>
            dialogs.confirm({
              title: t('Close {n} ports on {server}?', { n: count, server: target }),
              description: t('The agents of that server are told the ports are closed.'),
              actionLabel: t('Close'),
              onConfirm: () => panel.closeProfilePorts(p),
            }),
        })
      }
      items.push({
        id: 'srv:' + p.name,
        group: 'servers',
        title: p.name,
        subtitle: p.destination,
        monoSubtitle: true,
        icon: Server,
        tone: p.running ? 'emerald' : 'zinc',
        meta: counter,
        fields: [p.name, p.destination],
        disabled: !p.running && !p.current,
        actions,
        data: { kind: 'server', profile: p },
      })
    }

    // Commands.
    const command = (
      id: string,
      title: string,
      icon: LucideIcon,
      tone: Tone,
      description: string,
      run: () => unknown,
      extra: Partial<SpotItem> & { away?: boolean; keepOpen?: boolean; href?: string } = {},
    ) => {
      const { away, keepOpen, href, fields = [], ...rest } = extra
      items.push({
        id: 'cmd:' + id,
        group: 'commands',
        title,
        icon,
        tone,
        rank: items.length,
        actions: [{ id: 'run', label: title, icon, run, away, keepOpen, href }],
        data: { kind: 'command', description, keys: rest.keys },
        ...rest,
        fields: [title, rest.subtitle || '', ...fields, description],
      })
    }

    const tabs: [Tab, string, LucideIcon, string, string | undefined][] = [
      ['open', t('Show open ports'), ArrowLeftRight, t('The ports forwarded to this computer.'), '1'],
      ['det', t('Show ports detected on the server'), Radar, t('Listening ports of the server that no forward uses yet.'), '2'],
      ['closed', t('Show closed ports'), History, t('Ports closed earlier, ready to reopen.'), '3'],
      ['log', t('Show activity'), Activity, t('What happened, newest first.'), '4'],
    ]
    for (const [tab, title, icon, description, key] of tabs) {
      command('tab:' + tab, title, icon, 'brand', description, () => onTab(tab), { keys: key ? [key] : undefined })
    }
    command(
      'terminal',
      t('Open terminal'),
      SquareTerminal,
      'zinc',
      t('Opens a shell on the connected server. Anything you type runs there. It ends when you close it or stop the Companion.'),
      openTerminal,
      { away: true, fields: ['shell ssh'] },
    )
    command('browse', t('Ports on the server'), ScanSearch, 'sky', t('List the listening ports of the server and open one.'), () => setQuery(':'), {
      keepOpen: true,
    })
    command('scan', t('Scan the server again'), RefreshCw, 'sky', t('Look for listening ports on the server now.'), () => panel.loadDetected(true))

    const themeIcons: Record<ThemeChoice, LucideIcon> = { system: SunMoon, light: Sun, dark: Moon }
    for (const choice of ['light', 'dark', 'system'] as ThemeChoice[]) {
      command(
        'theme:' + choice,
        t('Theme: {theme}', { theme: themeLabel(t, choice) }),
        themeIcons[choice],
        'zinc',
        t('Use the light, dark or system appearance.'),
        () => theme.setChoice(choice),
        { meta: theme.choice === choice ? t('Current') : undefined, fields: ['appearance'] },
      )
    }
    for (const code of LANGS) {
      command(
        'lang:' + code,
        t('Language') + ': ' + LANG_NAMES[code],
        Languages,
        'zinc',
        t('Show the panel in this language.'),
        () => setLang(code),
        { meta: lang === code ? t('Current') : undefined, fields: [code, 'language'] },
      )
    }
    command('policy:ask', t('Ask every time'), ShieldCheck, 'zinc', t('Each port the agent asks for waits for your approval.'), () => setPolicy('ask'), {
      subtitle: t('When to ask for approval'),
      meta: policy === 'ask' ? t('Current') : undefined,
    })
    command(
      'policy:session',
      t('Remember for this session'),
      ShieldCheck,
      'zinc',
      t('A port you already approved reopens without asking until the session ends.'),
      () => setPolicy('session'),
      { subtitle: t('When to ask for approval'), meta: policy === 'session' ? t('Current') : undefined },
    )
    const notify = panel.settings.notify
    command(
      'notify',
      notify ? t('Turn notifications off') : t('Turn notifications on'),
      notify ? BellOff : Bell,
      'zinc',
      t('Notify me when an agent asks for a port'),
      () => panel.saveSettings({ notify: !notify }),
      { subtitle: t('Notifications'), meta: notify ? t('On') : t('Off') },
    )
    const autoOpen = panel.settings.auto_open
    command(
      'autoopen',
      t('Open the panel automatically'),
      PanelTopOpen,
      'zinc',
      t('When an agent asks and no panel is open, open it in the browser.'),
      () => panel.saveSettings({ auto_open: !autoOpen }),
      { meta: autoOpen ? t('On') : t('Off') },
    )
    if (panel.forwards.length) {
      const n = panel.forwards.length
      command(
        'closeall',
        t('Close all on {server}', { server }),
        X,
        'red',
        t('Every open port on this server closes. Agents are told that the ports are gone.'),
        () =>
          dialogs.confirm({
            title: t('Close all {n} ports?', { n }),
            description: t('Every open port on this server closes. Agents are told that the ports are gone.'),
            actionLabel: t('Close all'),
            onConfirm: () => panel.closeAll(),
          }),
        { away: true, danger: true },
      )
    }
    command('connect', t('Connect to a server'), Plug, 'emerald', t('To connect another server, run agentport setup in a terminal.'), () => dialogs.openConnect(), {
      away: true,
      fields: ['server setup'],
    })

    const topics: ['about' | 'start' | 'mcp' | 'sec' | 'trouble', LucideIcon, string, string][] = [
      ['about', Info, t('What agentport is'), t('How it works, in three steps')],
      ['start', Play, t('First steps'), t('Install, connect and use with your agent')],
      ['mcp', Plug, t('MCP tools'), t('What the agent can and cannot do')],
      ['sec', Lock, t('Security and privacy'), t('Where ports listen and what leaves the machine')],
      ['trouble', TriangleAlert, t('Troubleshooting'), t('Busy port, dropped tunnel, agent without MCP')],
    ]
    for (const [tab, icon, title, description] of topics) {
      command('help:' + tab, title, icon, 'sky', description, () => dialogs.openInfo(tab), { away: true, subtitle: t('Help'), fields: ['help about'] })
    }
    command('shortcuts', t('Keyboard shortcuts'), Keyboard, 'sky', t('Every key the panel answers to.'), () => dialogs.openShortcuts(), {
      away: true,
      keys: ['?'],
      fields: ['keys help'],
    })
    command('github', t('Project on GitHub'), ExternalLink, 'zinc', PROJECT_LABEL, () => openInNewTab(PROJECT_URL), {
      href: PROJECT_URL,
      subtitle: PROJECT_LABEL,
      monoSubtitle: true,
      fields: ['github source'],
    })
    command('issues', t('Report a problem'), Bug, 'zinc', PROJECT_LABEL + '/issues', () => openInNewTab(ISSUES_URL), {
      href: ISSUES_URL,
      fields: ['github issue bug'],
    })
    command(
      'stop',
      stop.armed ? t('Press Enter again to stop the Companion') : t('Stop the Companion'),
      Power,
      'red',
      t('Stop the Companion (closes every port)'),
      () => {
        if (stop.press()) close()
      },
      { keepOpen: true, danger: true, fields: ['quit shutdown'] },
    )

    // Activity, newest first.
    ;[...panel.activity]
      .reverse()
      .slice(0, 60)
      .forEach((a, index) => {
        items.push({
          id: 'act:' + a.at + ':' + index,
          group: 'activity',
          title: a.text,
          subtitle: fmtTime(lang, a.at),
          icon: Activity,
          tone: ACTIVITY_TONE[a.kind] || 'zinc',
          fields: [a.text],
          rank: index,
          actions: [{ id: 'show', label: t('Show activity'), icon: Activity, run: () => onTab('log') }],
          data: { kind: 'activity', entry: a },
        })
      })

    return items
  }, [t, lang, setLang, panel, dialogs, theme, policy, setPolicy, onTab, openTerminal, startRename, setQuery, close, stop])
}
