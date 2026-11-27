import { History, RotateCcw, SlidersHorizontal, X } from 'lucide-react'
import { Tip } from '@/components/tip'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import { useDialogs } from '@/lib/dialogs'
import { fmtDateTime } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import { usePanel } from '@/lib/panel'
import type { ClosedForward } from '@/lib/types'
import { EmptyState, ListCard, PortRoute } from './common'

export function ClosedPorts() {
  const { t, lang } = useI18n()
  const panel = usePanel()
  const dialogs = useDialogs()

  function reason(value: string) {
    switch (value) {
      case 'user':
        return t('You closed it')
      case 'agent':
        return t('The agent released it')
      case 'session_ended':
        return t('Agent ended')
      case 'expired':
        return t('Time ran out')
      default:
        return value
    }
  }

  function reopenWithChanges(c: ClosedForward) {
    dialogs.editPort({
      mode: 'reopen',
      remote: c.remote_port,
      local: c.local_port,
      label: c.label || '',
      path: c.path || '',
      closed: c,
    })
  }

  if (!panel.closed.length) {
    return (
      <ListCard>
        <EmptyState icon={<History />}>{t('Nothing closed yet.')}</EmptyState>
      </ListCard>
    )
  }

  return (
    <ListCard>
      <ScrollArea className="min-h-0 flex-1">
        {[...panel.closed].reverse().map((c) => (
          <div
            key={c.remote_port + ':' + c.closed_at}
            className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 border-b px-3 py-2 last:border-b-0 hover:bg-muted/30 sm:px-4 md:grid-cols-[13rem_minmax(0,1fr)_auto] md:gap-x-4"
          >
            <PortRoute remote={c.remote_port} local={c.local_port} className="hidden md:flex" />
            <div className="min-w-0">
              <PortRoute remote={c.remote_port} local={c.local_port} className="mb-0.5 text-xs md:hidden" />
              <div className="truncate text-sm font-semibold">{c.label || t('No name')}</div>
              <div className="truncate text-[13px] text-muted-foreground">
                {reason(c.reason)} · {fmtDateTime(lang, c.closed_at)}
              </div>
            </div>
            <div className="flex items-center gap-0.5">
              <Button variant="outline" size="sm" className="h-8 gap-1.5" onClick={() => void panel.reopen(c)}>
                <RotateCcw className="max-sm:hidden" />
                {t('Reopen')}
              </Button>
              <Button variant="ghost" size="sm" className="hidden h-8 lg:inline-flex" onClick={() => reopenWithChanges(c)}>
                {t('Reopen with changes')}
              </Button>
              <Tip label={t('Reopen with changes')}>
                <Button
                  variant="ghost"
                  size="icon"
                  className="size-8 text-muted-foreground lg:hidden"
                  aria-label={t('Reopen with changes')}
                  onClick={() => reopenWithChanges(c)}
                >
                  <SlidersHorizontal />
                </Button>
              </Tip>
              <Tip label={t('Remove from history')}>
                <Button
                  variant="ghost"
                  size="icon"
                  className="size-8 text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                  aria-label={t('Remove from history')}
                  onClick={() => void panel.forgetClosed(c)}
                >
                  <X />
                </Button>
              </Tip>
            </div>
          </div>
        ))}
      </ScrollArea>
    </ListCard>
  )
}
