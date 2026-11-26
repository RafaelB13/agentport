// Shapes of the Companion API the panel reads. They mirror internal/ports,
// internal/portsproto and internal/companion.

export type ForwardState = 'active' | 'reserved' | 'session_ended' | (string & {})

export interface Forward {
  remote_port: number
  local_port: number
  url: string
  label?: string
  path?: string
  group?: string
  origin: 'manual' | 'agent' | (string & {})
  state: ForwardState
  owners: string[] | null
  mine?: boolean
  pinned: boolean
  created_at: string
  uptime_seconds: number
}

export interface PendingPort {
  remote_port: number
  label?: string
  path?: string
}

export interface PendingRequest {
  id: string
  session: string
  agent: string
  cwd?: string
  reason: string
  group?: string
  ports: PendingPort[]
  created_at: string
}

export interface ClosedForward {
  remote_port: number
  local_port: number
  label?: string
  path?: string
  reason: string
  closed_at: string
}

export interface ActivityEntry {
  at: string
  kind: 'ok' | 'ask' | 'no' | 'info' | (string & {})
  text: string
}

export interface PortsState {
  forwards: Forward[] | null
  pending: PendingRequest[] | null
  closed: ClosedForward[] | null
  activity: ActivityEntry[] | null
}

export interface DetectedPort {
  port: number
  bind: string
  pid?: number
  process?: string
  cmdline?: string
  user?: string
  cwd?: string
  forwarded: boolean
  local_port?: number
}

export interface DetectedResponse {
  ports: DetectedPort[] | null
  updated_at: string
  error?: string
}

export interface LocalCheck {
  port: number
  status: 'free' | 'open_port' | 'other_profile' | 'process' | (string & {})
  remote_port?: number
  process?: string
  pid?: number
  kind?: string
  stoppable: boolean
}

export interface LocalHealth {
  port: number
  tcp: boolean
  tcp_error?: string
  http_status?: number
  http_latency_ms?: number
  checked_at: string
}

export interface TunnelStatus {
  connected: boolean
  last_error?: string
  updated_at?: string
}

export interface CompanionStatus {
  profile: string
  destination?: string
  version?: string
  started_at?: string
  tunnel?: TunnelStatus
}

export interface PanelSettings {
  auto_open: boolean
  notify: boolean
}

export interface ProfileEntry {
  name: string
  destination: string
  current: boolean
  running: boolean
  url?: string
  forwards?: number
}

export interface PortResult {
  remote_port: number
  state: string
  local_port?: number
  url?: string
  message?: string
}

export type Duration = 'session' | '1h' | 'pin'
