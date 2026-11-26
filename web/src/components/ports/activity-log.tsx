import { Activity } from 'lucide-react'
import { ScrollArea } from '@/components/ui/scroll-area'
import { fmtTime } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import { usePanel } from '@/lib/panel'
import { cn } from '@/lib/utils'
import { EmptyState, ListCard } from './common'

const KIND_DOT: Record<string, string> = {
  ok: 'bg-emerald-500',
  ask: 'bg-amber-500',
  no: 'bg-red-500',
  info: 'bg-sky-500',
}

// ActivityLog shows what happened, newest first.
export function ActivityLog() {
  const { t, lang } = useI18n()
  const { activity } = usePanel()
  if (!activity.length) {
    return (
      <ListCard>
        <EmptyState icon={<Activity />}>{t('No activity yet.')}</EmptyState>
      </ListCard>
    )
  }
  return (
    <ListCard>
      <ScrollArea className="min-h-0 flex-1">
        <ol>
          {[...activity].reverse().map((a, index) => (
            <li key={a.at + ':' + index} className="flex items-baseline gap-3 border-b px-3 py-2 text-sm last:border-b-0 sm:px-4">
              <time dateTime={a.at} className="w-20 shrink-0 font-mono text-xs tabular-nums text-muted-foreground">
                {fmtTime(lang, a.at)}
              </time>
              <span className={cn('size-2 shrink-0 translate-y-[-1px] rounded-full', KIND_DOT[a.kind] || 'bg-muted-foreground/50')} aria-hidden="true" />
              <span className="min-w-0 break-words">{a.text}</span>
            </li>
          ))}
        </ol>
      </ScrollArea>
    </ListCard>
  )
}
