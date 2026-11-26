import { Check, ChevronsUpDown, Plus, X } from 'lucide-react'
import { StatusDot } from '@/components/brand'
import { Tip } from '@/components/tip'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useDialogs } from '@/lib/dialogs'
import { fmtDuration } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import { usePanel } from '@/lib/panel'
import { cn } from '@/lib/utils'

export function ServerSwitcher() {
  const { t } = useI18n()
  const panel = usePanel()
  const dialogs = useDialogs()
  const s = panel.status
  const connected = Boolean(s?.tunnel?.connected) && !panel.offline
  const host = s ? s.destination || s.profile : '—'
  const started = s?.started_at ? Date.parse(s.started_at) : NaN
  const uptime = Number.isNaN(started) ? 0 : (Date.now() - started) / 1000
  const detail = s ? t('· profile {name} · {uptime}', { name: s.profile, uptime: fmtDuration(t, uptime) }) : ''

  return (
    <DropdownMenu onOpenChange={(open) => open && void panel.loadProfiles()}>
      <Tip
        label={
          <span className="flex flex-col gap-0.5">
            <span className="font-medium">{t('Switch server')}</span>
            <span className="text-muted-foreground">
              {connected ? t('Connected') : t('Disconnected')}
              {s ? ' · ' + host : ''}
            </span>
          </span>
        }
      >
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            className="h-9 min-w-0 max-w-full justify-start gap-2 px-2 data-[state=open]:bg-accent"
            aria-label={t('Switch server')}
          >
            <StatusDot ok={connected} />
            <span className="min-w-0 truncate font-mono text-[13px] font-medium">{host}</span>
            {detail && (
              <span className="hidden min-w-0 truncate text-xs font-normal text-muted-foreground lg:inline">{detail}</span>
            )}
            <ChevronsUpDown className="size-3.5 shrink-0 opacity-60" />
          </Button>
        </DropdownMenuTrigger>
      </Tip>
      <DropdownMenuContent align="start" className="w-[22rem] max-w-[calc(100vw-2rem)]">
        <DropdownMenuLabel className="text-xs font-normal text-muted-foreground">
          {t('Servers on this computer')}
        </DropdownMenuLabel>
        {panel.profiles.length === 0 && (
          <p className="px-2 py-1.5 text-sm text-muted-foreground">{t('No servers are set up yet.')}</p>
        )}
        {panel.profiles.map((p) => {
          const count = p.forwards ?? 0
          const counter = p.running ? (count === 1 ? t('1 port') : t('{n} ports', { n: count })) : t('stopped')
          const server = p.destination || p.name
          const closable = p.running && count > 0 && !p.current
          return (
            <div key={p.name} className="flex items-center gap-0.5">
              <DropdownMenuItem
                className="min-w-0 flex-1 gap-2.5"
                disabled={!p.running && !p.current}
                aria-current={p.current}
                onSelect={() => dialogs.switchTo(p)}
              >
                <span
                  className={cn('size-2 shrink-0 rounded-full', p.running ? 'bg-emerald-500' : 'bg-muted-foreground/40')}
                  aria-hidden="true"
                />
                <span className="min-w-0 flex-1">
                  <span className="block truncate font-medium" title={p.name}>
                    {p.name}
                  </span>
                  {p.destination && (
                    <span className="block truncate font-mono text-xs text-muted-foreground" title={p.destination}>
                      {p.destination}
                    </span>
                  )}
                </span>
                <span className="shrink-0 text-xs tabular-nums text-muted-foreground">{counter}</span>
                <Check className={cn('size-4 shrink-0', p.current ? 'opacity-100' : 'opacity-0')} aria-hidden="true" />
              </DropdownMenuItem>
              {closable ? (
                <Tip label={t('Close the ports of {server}', { server })} side="right">
                  <DropdownMenuItem
                    className="size-8 shrink-0 justify-center p-0 text-muted-foreground focus:text-destructive"
                    aria-label={t('Close the ports of {server}', { server })}
                    onSelect={() =>
                      dialogs.confirm({
                        title: t('Close {n} ports on {server}?', { n: count, server }),
                        description: t('The agents of that server are told the ports are closed.'),
                        actionLabel: t('Close'),
                        onConfirm: () => panel.closeProfilePorts(p),
                      })
                    }
                  >
                    <X className="size-4" />
                  </DropdownMenuItem>
                </Tip>
              ) : (
                <span className="size-8 shrink-0" aria-hidden="true" />
              )}
            </div>
          )
        })}
        <DropdownMenuSeparator />
        <DropdownMenuItem onSelect={() => dialogs.openConnect()}>
          <Plus className="size-4" />
          {t('Connect to a server')}
        </DropdownMenuItem>
        <p className="px-2 pb-1.5 pt-1 text-xs leading-snug text-muted-foreground">
          {t('To connect another server, run agentport setup in a terminal.')}
        </p>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
