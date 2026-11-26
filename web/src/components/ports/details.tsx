import { useEffect, useState } from 'react'
import { Activity, CircleCheck, CircleX } from 'lucide-react'
import { errorText } from '@/lib/api'
import { fmtDateTime, fmtDuration } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import { usePanel } from '@/lib/panel'
import type { DetectedPort, Forward, LocalHealth } from '@/lib/types'
import { DetailList } from './common'

// ProcessDetails lists what the server knows about the process on a port.
export function ProcessDetails({ info }: { info: DetectedPort }) {
  const { t } = useI18n()
  return (
    <DetailList
      items={[
        [t('Process'), info.process || '-'],
        [t('PID'), info.pid ? String(info.pid) : '-'],
        [t('User'), info.user || '-'],
        [t('Bind'), info.bind || '-'],
        [t('Command line'), info.cmdline || '-'],
        [t('Working directory'), info.cwd || '-'],
      ]}
    />
  )
}

export function ForwardDetails({ forward: f }: { forward: Forward }) {
  const { t, lang } = useI18n()
  const { portInfo } = usePanel()
  const owners = (f.owners || []).length ? (f.owners || []).join(', ') : t('nobody (the port is kept by you)')
  const info = portInfo.get(f.remote_port)
  return (
    <div className="space-y-4 border-t bg-muted/30 px-4 py-3 md:pl-[3.25rem]">
      <DetailList
        items={[
          [t('Remote port'), ':' + f.remote_port],
          [t('Local address'), 'localhost:' + f.local_port],
          [t('Started'), fmtDateTime(lang, f.created_at)],
          [t('Uptime'), fmtDuration(t, f.uptime_seconds)],
          [t('Owners'), owners],
          [t('Origin'), f.origin === 'manual' ? t('You opened it') : t('An agent asked for it')],
          [t('State'), f.state],
        ]}
      />
      {info ? (
        <ProcessDetails info={info} />
      ) : (
        <p className="text-xs text-muted-foreground">{t('Process details appear once the port is detected on the server.')}</p>
      )}
      <HealthLine port={f.local_port} />
    </div>
  )
}

// HealthLine shows whether the local end of a forward answers.
function HealthLine({ port }: { port: number }) {
  const { t } = useI18n()
  const { healthOf } = usePanel()
  const [health, setHealth] = useState<LocalHealth | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    let current = true
    healthOf(port)
      .then((result) => current && setHealth(result))
      .catch((reason) => current && setError(errorText(reason)))
    return () => {
      current = false
    }
  }, [port, healthOf])

  if (error) return <p className="text-xs text-destructive">{error}</p>
  if (!health) {
    return (
      <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
        <Activity className="size-3.5 motion-safe:animate-pulse" aria-hidden="true" />
        {t('Checking…')}
      </p>
    )
  }
  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs">
      <span className="flex items-center gap-1.5">
        {health.tcp ? (
          <CircleCheck className="size-3.5 text-emerald-600 dark:text-emerald-400" aria-hidden="true" />
        ) : (
          <CircleX className="size-3.5 text-destructive" aria-hidden="true" />
        )}
        {health.tcp ? t('TCP ok') : t('TCP not reachable: {error}', { error: health.tcp_error || '-' })}
      </span>
      {health.http_status ? (
        <span className="text-muted-foreground">
          {t('HTTP status {status}, answered in {ms} ms', { status: health.http_status, ms: health.http_latency_ms ?? 0 })}
        </span>
      ) : null}
    </div>
  )
}
