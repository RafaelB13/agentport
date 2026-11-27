import { useState } from 'react'
import { ChevronDown, LoaderCircle, Radar, TriangleAlert } from 'lucide-react'
import { Tip } from '@/components/tip'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import { useI18n } from '@/lib/i18n'
import { usePanel } from '@/lib/panel'
import { cn } from '@/lib/utils'
import { EmptyState, ListCard } from './common'
import { ProcessDetails } from './details'

// DetectedPorts lists the server's listening ports that no forward uses yet.
export function DetectedPorts() {
  const { t } = useI18n()
  const panel = usePanel()
  const [expanded, setExpanded] = useState<Set<number>>(new Set())
  const [opening, setOpening] = useState<number | null>(null)
  const list = panel.detected

  if (panel.detectedBusy && !list.length) {
    return (
      <ListCard>
        <EmptyState icon={<LoaderCircle className="motion-safe:animate-spin" />}>{t('Scanning the server…')}</EmptyState>
      </ListCard>
    )
  }

  return (
    <ListCard>
      {panel.detectedError && (
        <div className="flex shrink-0 items-start gap-2 border-b bg-destructive/5 px-4 py-2.5 text-sm text-destructive">
          <TriangleAlert className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
          <span className="min-w-0 break-words">{t('The server could not be scanned: {error}', { error: panel.detectedError })}</span>
        </div>
      )}
      {!list.length && !panel.detectedError ? (
        <EmptyState icon={<Radar />}>{t('No other listening ports were found on the server.')}</EmptyState>
      ) : (
        <ScrollArea className="min-h-0 flex-1">
          {list.map((p) => {
            const open = expanded.has(p.port)
            return (
              <div key={p.port} className="border-b last:border-b-0">
                <div className="grid grid-cols-[3.75rem_minmax(0,1fr)_auto] items-center gap-x-3 px-3 py-2 hover:bg-muted/30 sm:px-4 md:grid-cols-[5.5rem_minmax(10rem,15rem)_minmax(0,1fr)_auto] md:gap-x-4">
                  <span className="font-mono text-sm font-semibold tabular-nums">:{p.port}</span>
                  <div className="min-w-0">
                    <div className="truncate text-sm font-semibold" title={p.process}>
                      {p.process || t('Unknown process')}
                    </div>
                    <div className="break-words font-mono text-[13px] text-muted-foreground sm:truncate">
                      {[p.bind, p.user].filter(Boolean).join(' · ')}
                    </div>
                  </div>
                  <div className="hidden min-w-0 md:block">
                    {p.cmdline && (
                      <Tip side="top" label={<span className="block max-w-xl break-all font-mono">{p.cmdline}</span>}>
                        <p className="truncate font-mono text-[13px] text-muted-foreground">{p.cmdline}</p>
                      </Tip>
                    )}
                  </div>
                  <div className="flex items-center gap-0.5">
                    <Button
                      variant="outline"
                      size="sm"
                      className="h-8"
                      disabled={opening === p.port}
                      onClick={async () => {
                        setOpening(p.port)
                        await panel.openPort(p.port)
                        await panel.loadDetected(true)
                        setOpening(null)
                      }}
                    >
                      {opening === p.port && <LoaderCircle className="motion-safe:animate-spin" />}
                      {t('Open port')}
                    </Button>
                    <Tip label={t('Details')}>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="size-8 text-muted-foreground"
                        aria-label={t('Details')}
                        aria-expanded={open}
                        onClick={() =>
                          setExpanded((current) => {
                            const next = new Set(current)
                            if (next.has(p.port)) next.delete(p.port)
                            else next.add(p.port)
                            return next
                          })
                        }
                      >
                        <ChevronDown className={cn('transition-transform duration-200', open && 'rotate-180')} />
                      </Button>
                    </Tip>
                  </div>
                </div>
                {open && (
                  <div className="border-t bg-muted/30 px-4 py-3 md:pl-[7.5rem]">
                    <ProcessDetails info={p} />
                  </div>
                )}
              </div>
            )
          })}
        </ScrollArea>
      )}
    </ListCard>
  )
}
