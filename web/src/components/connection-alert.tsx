import { TriangleAlert, Unplug } from 'lucide-react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { useI18n } from '@/lib/i18n'
import { usePanel } from '@/lib/panel'

// ConnectionAlert says when the Companion does not answer or its tunnel is
// down, with the tunnel's last error in full (the status bar only names it).
export function ConnectionAlert() {
  const { t } = useI18n()
  const { status, offline } = usePanel()
  if (offline) {
    return (
      <Alert variant="destructive" className="shrink-0 bg-destructive/5 py-2.5 motion-safe:animate-in motion-safe:fade-in-0">
        <Unplug className="size-4" />
        <AlertTitle className="mb-0 leading-5">{t('The Companion is not answering.')}</AlertTitle>
      </Alert>
    )
  }
  if (!status || status.tunnel?.connected) return null
  const error = status.tunnel?.last_error
  return (
    <Alert variant="destructive" className="shrink-0 bg-destructive/5 py-2.5 motion-safe:animate-in motion-safe:fade-in-0">
      <TriangleAlert className="size-4" />
      <AlertTitle className="leading-5">{t('Disconnected')}</AlertTitle>
      <AlertDescription className="space-y-1 text-xs text-foreground/80">
        <p>{t('Dropped tunnel: the Companion reconnects by itself; the status shows it.')}</p>
        {error && <p className="break-words font-mono text-xs text-destructive">{error}</p>}
      </AlertDescription>
    </Alert>
  )
}
