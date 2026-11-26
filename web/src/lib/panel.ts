import { createContext, useContext } from 'react'
import type {
  ActivityEntry,
  ClosedForward,
  CompanionStatus,
  DetectedPort,
  Duration,
  Forward,
  LocalCheck,
  LocalHealth,
  PanelSettings,
  PendingRequest,
  ProfileEntry,
} from './types'

export type ConflictChoice = 'remap' | 'stop'

export interface ForwardChanges {
  label?: string
  path?: string
  local_port?: number
  duration?: string
}

export interface ReopenChanges {
  label?: string
  path?: string
  local_port?: number
  duration?: string
}

export interface Panel {
  status: CompanionStatus | null
  settings: PanelSettings
  profiles: ProfileEntry[]
  forwards: Forward[]
  pending: PendingRequest[]
  closed: ClosedForward[]
  activity: ActivityEntry[]
  // offline is true when the Companion does not answer; live while the event
  // stream is connected (otherwise the panel polls).
  offline: boolean
  live: boolean
  loaded: boolean
  // portInfo is every listening port of the server, keyed by port; detected is
  // the ones no forward uses yet, in port order.
  portInfo: Map<number, DetectedPort>
  detected: DetectedPort[]
  detectedBusy: boolean
  detectedError: string
  // leaving are the open ports playing their exit before they close.
  leaving: Set<number>

  loadState: () => Promise<void>
  loadProfiles: () => Promise<void>
  loadDetected: (refresh: boolean) => Promise<void>
  saveSettings: (patch: Partial<PanelSettings>) => Promise<void>
  checkLocal: (port: number) => Promise<LocalCheck>
  healthOf: (port: number) => Promise<LocalHealth>
  openPort: (port: number) => Promise<boolean>
  closePorts: (remotes: number[]) => Promise<void>
  closeAll: () => Promise<void>
  togglePin: (forward: Forward) => Promise<void>
  rename: (forward: Forward, label: string) => Promise<void>
  editForward: (remote: number, changes: ForwardChanges) => Promise<void>
  reopen: (closed: ClosedForward, changes?: ReopenChanges) => Promise<void>
  forgetClosed: (closed: ClosedForward) => Promise<void>
  approve: (
    request: PendingRequest,
    selected: Set<number>,
    duration: Duration,
    conflicts?: Record<number, ConflictChoice>,
  ) => Promise<void>
  deny: (request: PendingRequest) => Promise<void>
  closeProfilePorts: (profile: ProfileEntry) => Promise<void>
  stopCompanion: () => Promise<void>
  // configureRequestPort keeps the local port chosen in the settings of a
  // requested port; it is used when the request is approved.
  configureRequestPort: (requestId: string, remote: number, local: number) => void
  requestPortLocal: (requestId: string, remote: number) => number | undefined
}

export const PanelContext = createContext<Panel | null>(null)

export function usePanel(): Panel {
  const value = useContext(PanelContext)
  if (!value) throw new Error('usePanel outside PanelProvider')
  return value
}

// prefersMotion is false when the visitor asks for reduced motion.
export function prefersMotion(): boolean {
  return !window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
}

export function sessionNames(forwards: Forward[]): string[] {
  const names = new Set<string>()
  for (const f of forwards) for (const owner of f.owners || []) if (owner !== 'user') names.add(owner)
  return [...names]
}
