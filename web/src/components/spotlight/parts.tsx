import type { ReactNode } from 'react'
import type { LucideIcon } from 'lucide-react'
import type { Tone } from '@/lib/spotlight'
import { cn } from '@/lib/utils'

// Icon tiles: small rounded squares with a soft gradient, like app icons.
const TONES: Record<Tone, string> = {
  brand: 'from-[#9587fa] to-[#6c58f0]',
  sky: 'from-sky-400 to-sky-600',
  emerald: 'from-emerald-400 to-emerald-600',
  amber: 'from-amber-400 to-orange-500',
  red: 'from-rose-500 to-red-600',
  zinc: 'from-slate-400 to-slate-600 dark:from-slate-500 dark:to-slate-700',
}

export function Tile({ icon: Icon, tone, size = 'md' }: { icon: LucideIcon; tone: Tone; size?: 'md' | 'lg' }) {
  return (
    <span
      className={cn(
        'flex shrink-0 items-center justify-center bg-gradient-to-b text-white shadow-[inset_0_1px_0_rgba(255,255,255,0.28),0_1px_2px_rgba(0,0,0,0.18)] ring-1 ring-inset ring-black/10',
        TONES[tone],
        size === 'lg' ? 'size-12 rounded-xl [&_svg]:size-6' : 'size-8 rounded-lg [&_svg]:size-4',
      )}
      aria-hidden="true"
    >
      <Icon strokeWidth={2.25} />
    </span>
  )
}

export function Kbd({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <kbd
      className={cn(
        'inline-flex h-5 min-w-5 select-none items-center justify-center rounded-[5px] border border-black/10 bg-black/[0.04] px-1 font-sans text-[11px] font-semibold leading-none text-muted-foreground dark:border-white/10 dark:bg-white/[0.07]',
        className,
      )}
    >
      {children}
    </kbd>
  )
}

export function Keys({ keys, className }: { keys: string[]; className?: string }) {
  return (
    <span className={cn('flex items-center gap-0.5', className)}>
      {keys.map((key) => (
        <Kbd key={key}>{key}</Kbd>
      ))}
    </span>
  )
}

// Hint is a key and what it does, for the footer bar.
export function Hint({ keys, label, className }: { keys: string[]; label: string; className?: string }) {
  return (
    <span className={cn('flex min-w-0 items-center gap-1.5', className)}>
      <Keys keys={keys} />
      <span className="truncate">{label}</span>
    </span>
  )
}
