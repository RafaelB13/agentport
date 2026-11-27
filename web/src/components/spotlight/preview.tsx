import type { ReactNode } from 'react'
import { Ellipsis, Pin } from 'lucide-react'
import { PortRoute } from '@/components/ports/common'
import { Badge } from '@/components/ui/badge'
import { closedReason, fmtDateTime, fmtDuration, fmtTime } from '@/lib/format'
import { useI18n, type Translate } from '@/lib/i18n'
import { MOD_KEY, type SpotAction, type SpotItem } from '@/lib/spotlight'
import type { Forward } from '@/lib/types'
import { cn } from '@/lib/utils'
import { Keys, Tile } from './parts'

function stateText(t: Translate, f: Forward): string {
  if (f.state === 'active') return t('Live')
  if (f.state === 'reserved') return t('Waiting for the tunnel')
  if (f.state === 'session_ended') return t('Agent ended')
  return f.state
}

type Row = [string, ReactNode, boolean?]

function Rows({ rows }: { rows: Row[] }) {
  const shown = rows.filter(([, value]) => value !== undefined && value !== null && value !== '')
  if (!shown.length) return null
  return (
    <dl className="grid grid-cols-[5.5rem_minmax(0,1fr)] gap-x-3 gap-y-1.5 text-xs">
      {shown.map(([label, value, mono]) => (
        <div key={label} className="contents">
          <dt className="pt-px font-medium text-muted-foreground">{label}</dt>
          <dd className={cn('min-w-0 break-words font-medium text-foreground', mono && 'break-all font-mono text-[11.5px] leading-[1.15rem]')}>
            {value}
          </dd>
        </div>
      ))}
    </dl>
  )
}

function Head({ item, children }: { item: SpotItem; children?: ReactNode }) {
  return (
    <div className="flex flex-col items-center gap-1.5 border-b border-black/[0.06] px-5 pb-3 pt-4 text-center dark:border-white/[0.06]">
      <Tile icon={item.icon} tone={item.tone} size="lg" />
      <div className="mt-1 line-clamp-2 max-w-full break-words text-[15px] font-semibold leading-snug">{item.title}</div>
      {children}
    </div>
  )
}

// SpotlightPreview shows what the highlighted result is, and its actions.
// In the list it offers the two keyboard actions and the way to the others;
// while the actions list is open, it shows only the details.
export function SpotlightPreview({
  item,
  showActions,
  onRun,
  onMore,
}: {
  item: SpotItem | null
  showActions: boolean
  onRun: (action: SpotAction) => void
  onMore: () => void
}) {
  const { t, lang } = useI18n()
  if (!item) return null
  const d = item.data
  let head: ReactNode = null
  let body: ReactNode = null

  switch (d.kind) {
    case 'port': {
      const f = d.forward
      const owners = (f.owners || []).filter((o) => o !== 'user')
      head = (
        <>
          <PortRoute remote={f.remote_port} local={f.local_port} className="justify-center text-xs" />
          {f.pinned && (
            <Badge variant="warning" className="gap-1 px-2 py-0 text-[11px]">
              <Pin className="size-3" aria-hidden="true" />
              {t('Pinned')}
            </Badge>
          )}
        </>
      )
      body = (
        <Rows
          rows={[
            [t('URL'), f.url + (f.path || ''), true],
            [t('State'), stateText(t, f)],
            [t('Origin'), f.origin === 'manual' ? t('You opened it') : t('An agent asked for it')],
            [t('Owners'), owners.join(', ')],
            [t('Uptime'), fmtDuration(t, f.uptime_seconds)],
            [t('Process'), d.info?.process],
            [t('Command line'), d.info?.cmdline, true],
            [t('Working directory'), d.info?.cwd, true],
          ]}
        />
      )
      break
    }
    case 'detected': {
      const p = d.port
      head = <PortRoute remote={p.port} className="justify-center text-xs" />
      body = (
        <Rows
          rows={[
            [t('Process'), p.process || t('Unknown process')],
            [t('PID'), p.pid ? String(p.pid) : ''],
            [t('User'), p.user],
            [t('Bind'), p.bind, true],
            [t('Command line'), p.cmdline, true],
            [t('Working directory'), p.cwd, true],
          ]}
        />
      )
      break
    }
    case 'closed': {
      const c = d.closed
      head = <PortRoute remote={c.remote_port} local={c.local_port} className="justify-center text-xs" />
      body = (
        <Rows
          rows={[
            [t('State'), closedReason(t, c.reason)],
            [t('Closed at'), fmtDateTime(lang, c.closed_at)],
            [t('Path'), c.path, true],
          ]}
        />
      )
      break
    }
    case 'request': {
      const r = d.request
      head = <span className="text-xs text-muted-foreground">{[r.agent, r.cwd].filter(Boolean).join(' · ')}</span>
      body = (
        <div className="space-y-3 text-xs">
          {r.reason && <p className="font-medium leading-relaxed">{r.reason}</p>}
          <ul className="space-y-1.5">
            {r.ports.map((p) => (
              <li key={p.remote_port} className="flex min-w-0 items-center gap-2">
                <PortRoute remote={p.remote_port} local={p.remote_port} className="text-xs" />
                {(p.label || p.path) && (
                  <span className="min-w-0 truncate text-muted-foreground">{[p.label, p.path].filter(Boolean).join(' · ')}</span>
                )}
              </li>
            ))}
          </ul>
        </div>
      )
      break
    }
    case 'server': {
      const p = d.profile
      head = <span className="max-w-full truncate font-mono text-xs text-muted-foreground">{p.destination}</span>
      body = (
        <Rows
          rows={[
            [t('State'), p.current ? t('Current') : p.running ? t('Running') : t('stopped')],
            [t('Ports open'), p.running ? String(p.forwards ?? 0) : ''],
          ]}
        />
      )
      break
    }
    case 'command':
      body = (
        <div className="space-y-3 text-xs">
          <p className="font-medium leading-relaxed text-muted-foreground">{d.description}</p>
          {d.keys && (
            <p className="flex items-center gap-2 text-muted-foreground">
              {t('Shortcut')}
              <Keys keys={d.keys} />
            </p>
          )}
        </div>
      )
      break
    case 'activity':
      head = <span className="text-xs text-muted-foreground">{fmtTime(lang, d.entry.at)}</span>
      body = <p className="text-xs font-medium leading-relaxed text-muted-foreground">{t('Shown in the Activity tab.')}</p>
      break
    case 'new':
      head = d.valid ? <PortRoute remote={d.port} local={d.port} className="justify-center text-xs" /> : null
      body = (
        <p className="text-xs font-medium leading-relaxed text-muted-foreground">
          {d.valid ? t('Forward this port of the server to this computer.') : t('Use a number between 1 and 65535.')}
        </p>
      )
      break
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <Head item={item}>{head}</Head>
      <div className="min-h-0 flex-1 overflow-y-auto px-5 py-3.5">{body}</div>
      {showActions && !item.disabled && item.actions.length > 0 && (
        <div className="grid border-t border-black/[0.06] p-1.5 dark:border-white/[0.06]">
          {item.actions.slice(0, 2).map((action, index) => (
            <button
              key={action.id}
              type="button"
              tabIndex={-1}
              className={cn(
                'flex h-7 min-w-0 items-center gap-2 rounded-md px-2 text-left text-xs font-semibold transition-colors hover:bg-foreground/[0.06] [&_svg]:size-3.5 [&_svg]:shrink-0',
                action.destructive ? 'text-destructive' : 'text-foreground',
              )}
              onClick={() => onRun(action)}
            >
              <action.icon className={cn(!action.destructive && 'text-muted-foreground')} />
              <span className="min-w-0 flex-1 truncate">{action.label}</span>
              {index === 0 && <Keys keys={['↵']} />}
              {index === 1 && <Keys keys={[MOD_KEY, '↵']} />}
            </button>
          ))}
          {item.actions.length > 2 && (
            <button
              type="button"
              tabIndex={-1}
              className="flex h-7 min-w-0 items-center gap-2 rounded-md px-2 text-left text-xs font-semibold text-muted-foreground transition-colors hover:bg-foreground/[0.06] hover:text-foreground"
              onClick={onMore}
            >
              <Ellipsis className="size-3.5 shrink-0" aria-hidden="true" />
              <span className="min-w-0 flex-1 truncate">{t('All actions')}</span>
              <Keys keys={['→']} />
            </button>
          )}
        </div>
      )}
    </div>
  )
}
