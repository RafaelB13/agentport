import { createContext, useContext } from 'react'
import type { LucideIcon } from 'lucide-react'
import { readStored, writeStored } from './storage'
import type { ActivityEntry, ClosedForward, DetectedPort, Forward, PendingRequest, ProfileEntry } from './types'

// The search (the Spotlight-style palette): what opens it, what it lists and
// how it ranks what it lists.

export interface Spotlight {
  isOpen: boolean
  // open shows the search, with query typed in when given.
  open: (query?: string) => void
  close: () => void
  toggle: () => void
}

export const SpotlightContext = createContext<Spotlight | null>(null)

export function useSpotlight(): Spotlight {
  const value = useContext(SpotlightContext)
  if (!value) throw new Error('useSpotlight outside SpotlightProvider')
  return value
}

export type Tab = 'open' | 'det' | 'closed' | 'log' | 'term'

export type GroupId = 'top' | 'recent' | 'requests' | 'open' | 'det' | 'closed' | 'servers' | 'commands' | 'activity' | 'suggest'

export type Tone = 'brand' | 'sky' | 'emerald' | 'amber' | 'red' | 'zinc'

export interface SpotAction {
  id: string
  label: string
  icon: LucideIcon
  run: () => unknown
  destructive?: boolean
  // keepOpen leaves the search open: the action changes what it shows.
  keepOpen?: boolean
  // away is true when the action moves the focus elsewhere (a dialog, the
  // terminal), so closing the search must not put it back.
  away?: boolean
  // href is the address the action opens in a new tab.
  href?: string
}

export type SpotData =
  | { kind: 'port'; forward: Forward; info?: DetectedPort }
  | { kind: 'detected'; port: DetectedPort }
  | { kind: 'closed'; closed: ClosedForward }
  | { kind: 'request'; request: PendingRequest }
  | { kind: 'server'; profile: ProfileEntry }
  | { kind: 'command'; description: string; keys?: string[] }
  | { kind: 'activity'; entry: ActivityEntry }
  | { kind: 'new'; port: number; valid: boolean }

export interface SpotItem {
  id: string
  group: GroupId
  title: string
  subtitle?: string
  // monoSubtitle sets the subtitle in the code font (addresses, commands).
  monoSubtitle?: boolean
  icon: LucideIcon
  tone: Tone
  // meta is short text on the right of the row, such as the port.
  meta?: string
  // keys is a keyboard shortcut of the panel that does the same.
  keys?: string[]
  // fields are what the query matches, the title first.
  fields: string[]
  port?: number
  // rank orders the rows of a group when nothing is typed.
  rank?: number
  disabled?: boolean
  danger?: boolean
  // actions: the first runs on Enter, the second on Cmd/Ctrl+Enter, all of
  // them in the actions list (Right arrow or Tab).
  actions: SpotAction[]
  data: SpotData
}

export type Mode = 'all' | 'commands' | 'ports'

export interface ParsedQuery {
  mode: Mode
  text: string
  // port is the number typed, when the text is only digits.
  port: number | null
}

// parseQuery reads the prefixes: ">" lists only commands, ":" only ports.
export function parseQuery(raw: string): ParsedQuery {
  let text = raw.trim()
  let mode: Mode = 'all'
  if (text.startsWith('>')) {
    mode = 'commands'
    text = text.slice(1).trim()
  } else if (text.startsWith(':')) {
    mode = 'ports'
    text = text.slice(1).trim()
  }
  const port = mode !== 'commands' && /^\d+$/.test(text) ? Number(text) : null
  return { mode, text, port }
}

export function validPort(port: number | null): port is number {
  return port !== null && Number.isInteger(port) && port >= 1 && port <= 65535
}

function isWordChar(c: string | undefined): boolean {
  return c !== undefined && /[\p{L}\p{N}]/u.test(c)
}

// scoreText rates how well one word of the query matches a text: 0 is no
// match. Whole and prefix matches beat matches inside a word, and those beat
// letters found in order with gaps (the fuzzy match), which lose points for
// every gap.
export function scoreText(word: string, text: string, fuzzy = true): number {
  if (!word) return 1
  const q = word.toLowerCase()
  const s = text.toLowerCase()
  if (!s) return 0
  if (s === q) return 1000
  if (s.startsWith(q)) return 900 - Math.min(s.length - q.length, 80)
  const at = s.indexOf(q)
  if (at >= 0) return (isWordChar(s[at - 1]) ? 500 : 750) - Math.min(at, 100)
  if (!fuzzy || q.length < 2) return 0
  let from = 0
  let last = -2
  let score = 0
  for (const c of q) {
    const index = s.indexOf(c, from)
    if (index < 0) return 0
    if (index === last + 1) score += 12
    else if (!isWordChar(s[index - 1])) score += 8
    else score -= Math.min(index - from, 12)
    last = index
    from = index + 1
  }
  return score > 0 ? 100 + score : 0
}

// scoreItem rates an item for a query: every word must match one of its
// fields. The title counts more than the other fields, and only the title
// matches fuzzily: letters scattered through a long command line or
// description would match almost anything. A typed port number puts that
// exact port first.
export function scoreItem(item: SpotItem, query: ParsedQuery): number {
  if (query.port !== null && item.port !== undefined) {
    if (item.port === query.port) return 5000
    if (String(item.port).startsWith(query.text)) return 1500 - String(item.port).length
  }
  const words = query.text.split(/\s+/).filter(Boolean)
  if (!words.length) return 1
  let total = 0
  for (const word of words) {
    let best = 0
    item.fields.forEach((field, index) => {
      const value = index === 0 ? scoreText(word, field) : scoreText(word, field, false) * 0.8
      if (value > best) best = value
    })
    if (best <= 0) return 0
    total += best
  }
  return total / words.length
}

// Recent: the last items used, kept in this browser.
const RECENT_KEY = 'agentport.spotlight.recent'
const RECENT_MAX = 5

export function readRecent(): string[] {
  try {
    const value: unknown = JSON.parse(readStored(RECENT_KEY) || '[]')
    return Array.isArray(value) ? value.filter((id): id is string => typeof id === 'string').slice(0, RECENT_MAX * 2) : []
  } catch {
    return []
  }
}

export function rememberRecent(id: string): void {
  const next = [id, ...readRecent().filter((other) => other !== id)].slice(0, RECENT_MAX * 2)
  writeStored(RECENT_KEY, JSON.stringify(next))
}

export const RECENT_SHOWN = RECENT_MAX

// isMac picks the key names of the shortcuts: Cmd on Apple systems, Ctrl elsewhere.
export const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad|iPod/i.test(navigator.userAgent)

export const MOD_KEY = isMac ? '⌘' : 'Ctrl'
