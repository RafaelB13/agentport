import { useCallback, useEffect, useRef, useState } from 'react'
import { LoaderCircle, SquareTerminal } from 'lucide-react'
import { EmptyState, ListCard } from '@/components/ports/common'
import { Badge, type BadgeProps } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { BASE } from '@/lib/api'
import { useI18n, type Translate } from '@/lib/i18n'
import { useTheme } from '@/lib/theme'
import type { Screen } from './xterm-screen'

// The server terminal: a WebSocket to this Companion's /api/terminal attaches a
// shell on the server. Keystrokes go up and output comes down as binary
// frames; a text frame carries a resize. Closing the socket ends the ssh
// process there.

type TermState = 'idle' | 'connecting' | 'open' | 'ended' | 'unavailable'
type Failure = 'unavailable' | 'failed' | null

const BADGE: Record<TermState, BadgeProps['variant']> = {
  idle: 'muted',
  connecting: 'warning',
  open: 'success',
  ended: 'muted',
  unavailable: 'muted',
}

export function TerminalPanel({ active }: { active: boolean }) {
  const { t: translateNow } = useI18n()
  const { resolved } = useTheme()
  const [state, setState] = useState<TermState>('idle')
  const [failure, setFailure] = useState<Failure>(null)
  const [available, setAvailable] = useState<boolean | null>(null)
  const [opened, setOpened] = useState(false)
  const screenRef = useRef<HTMLDivElement>(null)
  const screen = useRef<Screen | null>(null)
  const socket = useRef<WebSocket | null>(null)
  const resizeTimer = useRef<ReturnType<typeof setTimeout>>()
  // A stable t that speaks the current language, for the socket's callbacks.
  const tRef = useRef(translateNow)
  tRef.current = translateNow
  const t = useCallback<Translate>((key, vars) => tRef.current(key, vars), [])

  const setTermState = useCallback((next: TermState, why: Failure = null) => {
    setState(next)
    setFailure(why)
  }, [])

  // The first time the tab shows, ask the Companion whether a terminal can run.
  useEffect(() => {
    if (!active || available !== null) return
    let current = true
    fetch(BASE + '/api/terminal', { cache: 'no-store' })
      .then(async (response) => (response.ok ? ((await response.json()) as { available?: boolean }) : {}))
      .catch(() => ({}) as { available?: boolean })
      .then((body) => {
        if (!current) return
        const ok = Boolean(body.available)
        setAvailable(ok)
        if (!ok) setTermState('unavailable', 'unavailable')
      })
    return () => {
      current = false
    }
  }, [active, available, setTermState])

  const theme = useCallback(() => {
    const el = screenRef.current
    const css = el ? getComputedStyle(el) : null
    const background = css?.backgroundColor || (resolved === 'dark' ? '#09090b' : '#ffffff')
    const foreground = css?.color || (resolved === 'dark' ? '#fafafa' : '#09090b')
    return {
      background,
      foreground,
      cursor: foreground,
      cursorAccent: background,
      selectionBackground: resolved === 'dark' ? 'rgba(255,255,255,0.22)' : 'rgba(9,9,11,0.16)',
    }
  }, [resolved])

  // The screen follows the panel's light and dark theme.
  useEffect(() => {
    screen.current?.setTheme(theme())
  }, [theme])

  const sendResize = useCallback(() => {
    const s = screen.current
    const ws = socket.current
    if (s && ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ type: 'resize', cols: s.term.cols, rows: s.term.rows }))
    }
  }, [])

  const fit = useCallback(() => {
    if (!screen.current?.fit()) return
    clearTimeout(resizeTimer.current)
    resizeTimer.current = setTimeout(sendResize, 60)
  }, [sendResize])

  useEffect(() => {
    const el = screenRef.current
    if (!el || typeof ResizeObserver !== 'function') return
    const observer = new ResizeObserver(() => fit())
    observer.observe(el)
    return () => observer.disconnect()
  }, [fit])

  useEffect(() => {
    if (active) fit()
  }, [active, fit])

  useEffect(
    () => () => {
      clearTimeout(resizeTimer.current)
      socket.current?.close(1000, 'closed by the panel')
      screen.current?.dispose()
    },
    [],
  )

  async function open() {
    if (socket.current || available !== true || !screenRef.current) return
    if (!screen.current) {
      try {
        const { createScreen } = await import('./xterm-screen')
        screen.current = createScreen(screenRef.current, theme(), (data) => {
          const ws = socket.current
          if (ws && ws.readyState === WebSocket.OPEN) ws.send(new TextEncoder().encode(data))
        })
      } catch {
        setTermState('idle', 'failed')
        return
      }
    }
    const s = screen.current
    s.fit()
    setTermState('connecting')
    const scheme = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const ws = new WebSocket(
      `${scheme}//${window.location.host}${BASE}/api/terminal?cols=${s.term.cols}&rows=${s.term.rows}`,
    )
    ws.binaryType = 'arraybuffer'
    socket.current = ws
    let connected = false
    ws.onopen = () => {
      if (socket.current !== ws) return
      connected = true
      setOpened(true)
      setTermState('open')
      sendResize()
      s.term.focus()
    }
    ws.onmessage = (event) => {
      if (socket.current === ws && event.data instanceof ArrayBuffer) s.term.write(new Uint8Array(event.data))
    }
    ws.onclose = () => {
      if (socket.current !== ws) return
      socket.current = null
      if (connected) {
        s.term.write('\r\n\x1b[2m' + t('Session ended') + '\x1b[0m\r\n')
        setTermState('ended')
      } else {
        setTermState('idle', 'failed')
      }
    }
  }

  // close ends the session: closing the socket ends the ssh process on the server.
  function close() {
    const ws = socket.current
    socket.current = null
    ws?.close(1000, 'closed by the panel')
    screen.current?.term.write('\r\n\x1b[2m' + t('Session closed') + '\x1b[0m\r\n')
    setTermState('idle')
  }

  const labels: Record<TermState, string> = {
    idle: t('Closed'),
    connecting: t('Connecting'),
    open: t('Connected'),
    ended: t('Session ended'),
    unavailable: t('Unavailable'),
  }
  const note =
    failure === 'unavailable'
      ? t('The server terminal is not available on this computer.')
      : failure === 'failed'
        ? t('The server terminal could not start. Check that the server is connected.')
        : t('Opens a shell on the connected server. Anything you type runs there. It ends when you close it or stop the Companion.')
  const canOpen = state === 'idle' || state === 'ended' || state === 'unavailable'

  return (
    <ListCard>
      <section aria-label={t('Terminal')} className="flex min-h-0 flex-1 flex-col">
        <div className="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-2 border-b px-3 py-2 sm:px-4">
          <Badge variant={BADGE[state]} className="gap-1.5" role="status" aria-live="polite">
            {state === 'connecting' ? (
              <LoaderCircle className="size-3 motion-safe:animate-spin" aria-hidden="true" />
            ) : (
              <span className="size-1.5 rounded-full bg-current" aria-hidden="true" />
            )}
            {labels[state]}
          </Badge>
          <p className={failure ? 'min-w-0 flex-1 text-xs text-destructive' : 'min-w-0 flex-1 text-xs text-muted-foreground'}>{note}</p>
          {canOpen && (
            <Button size="sm" className="h-8" disabled={state === 'unavailable' || available !== true} onClick={() => void open()}>
              <SquareTerminal />
              {state === 'ended' ? t('Reconnect') : t('Open terminal')}
            </Button>
          )}
          {(state === 'connecting' || state === 'open') && (
            <Button size="sm" variant="outline" className="h-8" onClick={close}>
              {t('Close terminal')}
            </Button>
          )}
        </div>
        <div className="relative min-h-0 flex-1 bg-background p-2">
          <div ref={screenRef} className="h-full w-full bg-background text-foreground" />
          {!opened && (
            <div className="absolute inset-0">
              <EmptyState icon={<SquareTerminal />}>{t('Terminal')}</EmptyState>
            </div>
          )}
        </div>
      </section>
    </ListCard>
  )
}
