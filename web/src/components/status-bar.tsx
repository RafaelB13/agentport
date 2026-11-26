import type { ReactNode } from 'react'
import { GitHubIcon } from '@/components/brand'
import { Tip } from '@/components/tip'
import { useI18n } from '@/lib/i18n'
import { PROJECT_LABEL, PROJECT_URL } from '@/lib/links'
import { sessionNames, usePanel } from '@/lib/panel'
import { cn } from '@/lib/utils'

function Item({ label, children, className }: { label: string; children: ReactNode; className?: string }) {
  return (
    <div className={cn('flex min-w-0 items-center gap-1.5', className)}>
      <dt className="shrink-0 text-muted-foreground">{label}</dt>
      <dd className="min-w-0 truncate font-medium tabular-nums text-foreground">{children}</dd>
    </div>
  )
}

// StatusBar is the footer: tunnel, agent sessions, open ports and this panel's
// address, then the version and the project link.
export function StatusBar() {
  const { t } = useI18n()
  const panel = usePanel()
  const s = panel.status
  const up = Boolean(s?.tunnel?.connected)
  const tunnelError = s && !up ? s.tunnel?.last_error || '' : ''
  const sessions = new Set(sessionNames(panel.forwards))
  for (const p of panel.pending) if (p.session) sessions.add(p.session)

  const tunnel = (
    <span className="flex min-w-0 items-center gap-1.5">
      <span
        className={cn('size-1.5 shrink-0 rounded-full', !s ? 'bg-muted-foreground/50' : up ? 'bg-emerald-500' : 'bg-red-500')}
        aria-hidden="true"
      />
      <span className={cn('min-w-0 truncate', s && !up && 'text-red-600 dark:text-red-400')}>
        {s ? (up ? t('active') : t('down')) : '—'}
      </span>
    </span>
  )

  return (
    <footer className="shrink-0 border-t bg-muted/30">
      <dl
        className="mx-auto flex h-9 w-full max-w-screen-2xl items-center gap-x-5 px-4 text-xs sm:px-6"
      >
        <Item label={t('Tunnel')} className="flex-initial">
          {tunnelError ? (
            <Tip side="top" label={<span className="block max-w-md break-words">{tunnelError}</span>}>
              <span tabIndex={0} className="flex min-w-0 rounded-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring">
                {tunnel}
              </span>
            </Tip>
          ) : (
            tunnel
          )}
        </Item>
        <Item label={t('Agent sessions')} className="hidden shrink-0 sm:flex">
          {t('{n} active', { n: sessions.size })}
        </Item>
        <Item label={t('Ports open')} className="shrink-0">
          {panel.forwards.length}
        </Item>
        <Item label={t('Panel')} className="hidden shrink-0 md:flex">
          <span className="font-mono font-normal">{window.location.host}</span>
        </Item>
        <div className="ml-auto flex shrink-0 items-center gap-3 text-muted-foreground">
          <span className="hidden lg:inline">agentport{s?.version ? ' ' + s.version : ''}</span>
          <a
            href={PROJECT_URL}
            target="_blank"
            rel="noopener noreferrer"
            className="flex items-center gap-1.5 hover:text-foreground"
            aria-label="GitHub"
          >
            <GitHubIcon className="size-3.5" />
            <span className="hidden xl:inline">{PROJECT_LABEL}</span>
          </a>
        </div>
      </dl>
    </footer>
  )
}
