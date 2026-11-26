import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { toast } from 'sonner'
import { api, BASE, errorText, portsApi } from '@/lib/api'
import { useI18n, type Translate } from '@/lib/i18n'
import {
  PanelContext,
  prefersMotion,
  type ConflictChoice,
  type ForwardChanges,
  type Panel,
  type ReopenChanges,
} from '@/lib/panel'
import type {
  ActivityEntry,
  ClosedForward,
  CompanionStatus,
  DetectedPort,
  DetectedResponse,
  Duration,
  Forward,
  LocalCheck,
  LocalHealth,
  PanelSettings,
  PendingRequest,
  PortResult,
  PortsState,
  ProfileEntry,
} from '@/lib/types'

const LEAVE_MS = 180
const CACHE_MS = 3000

type Cached<T> = { at: number; result: T }

function sleep(ms: number) {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

export function PanelProvider({ children }: { children: ReactNode }) {
  // t stays the same function across renders and always speaks the current
  // language, so the actions below need not change when the language does.
  const { t: translateNow } = useI18n()
  const tRef = useRef(translateNow)
  tRef.current = translateNow
  const t = useCallback<Translate>((key, vars) => tRef.current(key, vars), [])

  const [status, setStatus] = useState<CompanionStatus | null>(null)
  const [settings, setSettings] = useState<PanelSettings>({ auto_open: true, notify: true })
  const [profiles, setProfiles] = useState<ProfileEntry[]>([])
  const [forwards, setForwards] = useState<Forward[]>([])
  const [pending, setPending] = useState<PendingRequest[]>([])
  const [closed, setClosed] = useState<ClosedForward[]>([])
  const [activity, setActivity] = useState<ActivityEntry[]>([])
  const [offline, setOffline] = useState(false)
  const [live, setLive] = useState(false)
  const [loaded, setLoaded] = useState(false)
  const [portInfo, setPortInfo] = useState<Map<number, DetectedPort>>(new Map())
  const [detectedBusy, setDetectedBusy] = useState(false)
  const [detectedError, setDetectedError] = useState('')
  const [leaving, setLeaving] = useState<Set<number>>(new Set())

  const checks = useRef(new Map<number, Cached<LocalCheck>>())
  const health = useRef(new Map<number, Cached<LocalHealth>>())
  const requestConfig = useRef(new Map<string, number>())
  const liveRef = useRef(false)

  const loadState = useCallback(async () => {
    try {
      const state = await portsApi<PortsState>('GET', '/state')
      setForwards(state.forwards || [])
      setPending(state.pending || [])
      setClosed(state.closed || [])
      setActivity(state.activity || [])
      setOffline(false)
    } catch {
      setOffline(true)
    }
    setLoaded(true)
  }, [])

  const loadStatus = useCallback(async () => {
    try {
      setStatus(await api<CompanionStatus>('GET', '/api/status'))
    } catch {
      setStatus(null)
    }
  }, [])

  const loadSettings = useCallback(async () => {
    try {
      setSettings(await api<PanelSettings>('GET', '/api/settings'))
    } catch {
      // Keep the defaults shown.
    }
  }, [])

  const loadProfiles = useCallback(async () => {
    try {
      const data = await api<{ profiles: ProfileEntry[] | null }>('GET', '/api/profiles')
      setProfiles(data.profiles || [])
    } catch {
      setProfiles([])
    }
  }, [])

  // loadDetected scans the server through the Companion. refresh asks for a
  // scan now; without it the Companion answers from its short cache. The same
  // answer carries the forwarded ports, so open forwards get their process.
  const loadDetected = useCallback(async (refresh: boolean) => {
    setDetectedBusy(true)
    try {
      const data = await portsApi<DetectedResponse>('GET', '/detected?all=1' + (refresh ? '&refresh=1' : ''))
      setPortInfo(new Map((data.ports || []).map((p) => [p.port, p])))
      setDetectedError(data.error || '')
    } catch (error) {
      setDetectedError(errorText(error))
    }
    setDetectedBusy(false)
  }, [])

  const saveSettings = useCallback(
    async (patch: Partial<PanelSettings>) => {
      const next = { ...settings, ...patch }
      setSettings(next)
      try {
        setSettings(await api<PanelSettings>('PUT', '/api/settings', next))
        toast.success(t('Saved'))
      } catch (error) {
        toast.error(errorText(error))
        await loadSettings()
      }
    },
    [settings, loadSettings, t],
  )

  const checkLocal = useCallback(async (port: number) => {
    const cached = checks.current.get(port)
    if (cached && Date.now() - cached.at < CACHE_MS) return cached.result
    const result = await portsApi<LocalCheck>('GET', '/local/check?port=' + encodeURIComponent(port))
    checks.current.set(port, { at: Date.now(), result })
    return result
  }, [])

  const healthOf = useCallback(async (port: number) => {
    const cached = health.current.get(port)
    if (cached && Date.now() - cached.at < CACHE_MS) return cached.result
    const result = await portsApi<LocalHealth>('GET', '/local/health?port=' + encodeURIComponent(port))
    health.current.set(port, { at: Date.now(), result })
    return result
  }, [])

  const openPort = useCallback(
    async (port: number) => {
      let ok = false
      try {
        await portsApi('POST', '/forwards/open', { remote_port: port })
        toast.success(t('Opened :{port}', { port }))
        ok = true
      } catch (error) {
        toast.error(errorText(error))
      }
      await loadState()
      void loadDetected(false)
      return ok
    },
    [loadState, loadDetected, t],
  )

  // leave plays the exit of rows before their ports close.
  const leave = useCallback(async (remotes: number[]) => {
    if (!prefersMotion() || !remotes.length) return
    setLeaving(new Set(remotes))
    await sleep(LEAVE_MS)
  }, [])

  const closePorts = useCallback(
    async (remotes: number[]) => {
      await leave(remotes)
      try {
        const result = await portsApi<{ closed: number[] | null }>('POST', '/forwards/close', { remote_ports: remotes })
        toast.success(t('{n} ports closed', { n: (result.closed || []).length }))
      } catch (error) {
        toast.error(errorText(error))
      }
      await loadState()
      setLeaving(new Set())
    },
    [leave, loadState, t],
  )

  const closeAll = useCallback(async () => {
    await leave(forwards.map((f) => f.remote_port))
    try {
      const result = await portsApi<{ closed: number[] | null }>('POST', '/forwards/close-all', {})
      toast.success(t('{n} ports closed', { n: (result.closed || []).length }))
    } catch (error) {
      toast.error(errorText(error))
    }
    await loadState()
    setLeaving(new Set())
  }, [forwards, leave, loadState, t])

  const togglePin = useCallback(
    async (f: Forward) => {
      try {
        await portsApi('POST', `/forwards/${f.remote_port}/pin`, { pinned: !f.pinned })
        toast.success(
          f.pinned
            ? t('Unpinned :{port}', { port: f.remote_port })
            : t('Pinned :{port}', { port: f.remote_port }),
        )
      } catch (error) {
        toast.error(errorText(error))
      }
      await loadState()
    },
    [loadState, t],
  )

  const rename = useCallback(
    async (f: Forward, value: string) => {
      const label = value.trim()
      if (label === (f.label || '')) return
      try {
        await portsApi('POST', `/forwards/${f.remote_port}/rename`, { label })
        toast.success(t('Renamed'))
      } catch (error) {
        toast.error(errorText(error))
      }
      await loadState()
    },
    [loadState, t],
  )

  const editForward = useCallback(
    async (remote: number, changes: ForwardChanges) => {
      try {
        await portsApi('POST', `/forwards/${remote}/edit`, changes)
        toast.success(t('Saved'))
      } catch (error) {
        toast.error(errorText(error))
      }
      await loadState()
    },
    [loadState, t],
  )

  const reopen = useCallback(
    async (c: ClosedForward, changes?: ReopenChanges) => {
      try {
        await portsApi('POST', '/closed/reopen', {
          remote_port: c.remote_port,
          local_port: c.local_port,
          label: c.label || '',
          path: c.path || '',
          closed_at: c.closed_at,
          ...changes,
        })
        toast.success(t('Reopened :{port}', { port: c.remote_port }))
      } catch (error) {
        toast.error(errorText(error))
      }
      await loadState()
    },
    [loadState, t],
  )

  const forgetClosed = useCallback(
    async (c: ClosedForward) => {
      try {
        await portsApi('POST', '/closed/forget', { remote_port: c.remote_port, closed_at: c.closed_at })
      } catch (error) {
        toast.error(errorText(error))
      }
      await loadState()
    },
    [loadState],
  )

  const approve = useCallback(
    async (
      req: PendingRequest,
      selected: Set<number>,
      duration: Duration,
      conflicts: Record<number, ConflictChoice> = {},
    ) => {
      try {
        // A busy local port whose holder the visitor chose to stop is freed first.
        for (const [remote, choice] of Object.entries(conflicts)) {
          const port = Number(remote)
          if (choice !== 'stop' || !selected.has(port)) continue
          await portsApi('POST', '/forwards/stop-owner', { remote_port: port, local_port: port })
        }
        const body = {
          duration,
          ports: req.ports.map((p) => {
            if (!selected.has(p.remote_port)) return { remote_port: p.remote_port, skip: true }
            const local = requestConfig.current.get(req.id + ':' + p.remote_port)
            return local ? { remote_port: p.remote_port, local_port: local } : { remote_port: p.remote_port }
          }),
        }
        const result = await portsApi<{ results: PortResult[] | null }>(
          'POST',
          '/requests/' + encodeURIComponent(req.id) + '/approve',
          body,
        )
        for (const p of req.ports) requestConfig.current.delete(req.id + ':' + p.remote_port)
        const results = result.results || []
        const opened = results.filter((r) => r.state === 'active').length
        const failed = results.filter((r) => r.state === 'error')
        if (failed.length) {
          toast.error(t('{n} ports could not be opened.', { n: failed.length }) + ' ' + (failed[0].message || ''))
        } else {
          toast.success(t('{n} ports approved', { n: opened }))
        }
        checks.current.clear()
      } catch (error) {
        toast.error(errorText(error))
      }
      await loadState()
    },
    [loadState, t],
  )

  const deny = useCallback(
    async (req: PendingRequest) => {
      try {
        await portsApi('POST', '/requests/' + encodeURIComponent(req.id) + '/deny')
        toast.success(t('Denied'))
      } catch (error) {
        toast.error(errorText(error))
      }
      await loadState()
    },
    [loadState, t],
  )

  const closeProfilePorts = useCallback(
    async (p: ProfileEntry) => {
      try {
        await api('POST', '/api/profiles/close-all', { profile: p.name })
        toast.success(t('Ports closed on {server}', { server: p.destination || p.name }))
      } catch (error) {
        toast.error(errorText(error))
      }
      await loadProfiles()
    },
    [loadProfiles, t],
  )

  const stopCompanion = useCallback(async () => {
    try {
      await api('POST', '/api/stop', {})
      toast.success(t('The Companion is stopping. Its ports close.'))
      setOffline(true)
    } catch (error) {
      toast.error(errorText(error))
    }
  }, [t])

  const configureRequestPort = useCallback((requestId: string, remote: number, local: number) => {
    requestConfig.current.set(requestId + ':' + remote, local)
  }, [])

  const requestPortLocal = useCallback(
    (requestId: string, remote: number) => requestConfig.current.get(requestId + ':' + remote),
    [],
  )

  // Start: read everything once, then follow the event stream. The detected
  // list feeds the Detected count and the process names of forwards, so it
  // loads with the panel and again on every state change.
  useEffect(() => {
    void loadStatus()
    void loadSettings()
    void loadProfiles()
    void loadState()
    void loadDetected(false)
    const timers = [
      setInterval(loadStatus, 5000),
      setInterval(loadProfiles, 15000),
      setInterval(() => {
        if (!liveRef.current) void loadState()
      }, 3000),
    ]
    let events: EventSource | null = null
    if (typeof EventSource === 'function') {
      events = new EventSource(BASE + '/api/ports/events')
      events.addEventListener('state', () => {
        void loadState()
        void loadDetected(false)
      })
      events.onopen = () => {
        liveRef.current = true
        setLive(true)
      }
      events.onerror = () => {
        liveRef.current = false
        setLive(false)
      }
    }
    return () => {
      timers.forEach(clearInterval)
      events?.close()
    }
  }, [loadStatus, loadSettings, loadProfiles, loadState, loadDetected])

  const detected = useMemo(
    () => [...portInfo.values()].filter((p) => !p.forwarded).sort((a, b) => a.port - b.port),
    [portInfo],
  )

  const value: Panel = {
    status,
    settings,
    profiles,
    forwards,
    pending,
    closed,
    activity,
    offline,
    live,
    loaded,
    portInfo,
    detected,
    detectedBusy,
    detectedError,
    leaving,
    loadState,
    loadProfiles,
    loadDetected,
    saveSettings,
    checkLocal,
    healthOf,
    openPort,
    closePorts,
    closeAll,
    togglePin,
    rename,
    editForward,
    reopen,
    forgetClosed,
    approve,
    deny,
    closeProfilePorts,
    stopCompanion,
    configureRequestPort,
    requestPortLocal,
  }
  return <PanelContext.Provider value={value}>{children}</PanelContext.Provider>
}
