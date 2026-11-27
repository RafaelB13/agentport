import type { Translate } from './i18n'

export function fmtDuration(t: Translate, seconds: number | undefined): string {
  const s = Math.max(0, Math.round(Number(seconds) || 0))
  if (s < 60) return t('{n} s', { n: s })
  if (s < 3600) return t('{n} min', { n: Math.floor(s / 60) })
  return t('{h} h {m} min', { h: Math.floor(s / 3600), m: Math.floor((s % 3600) / 60) })
}

export function fmtTime(lang: string, value: string | undefined): string {
  const d = new Date(value ?? '')
  if (Number.isNaN(d.getTime())) return '—'
  return new Intl.DateTimeFormat(lang, { hour: '2-digit', minute: '2-digit', second: '2-digit' }).format(d)
}

export function fmtDateTime(lang: string, value: string | undefined): string {
  const d = new Date(value ?? '')
  if (Number.isNaN(d.getTime())) return '—'
  return new Intl.DateTimeFormat(lang, { dateStyle: 'medium', timeStyle: 'short' }).format(d)
}

// remoteProcessName is what the server runs behind a port: the process name,
// else the first word of its command line. Empty when the server has not said.
export function remoteProcessName(info: { process?: string; cmdline?: string } | undefined): string {
  if (!info) return ''
  if (info.process) return info.process
  const first = (info.cmdline || '').trim().split(/\s+/)[0] || ''
  return (first.split('/').pop() || '').slice(0, 32)
}

// closedReason says why a port closed, from the reason the Companion recorded.
export function closedReason(t: Translate, value: string): string {
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
