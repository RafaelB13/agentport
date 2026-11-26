import type { SVGProps } from 'react'
import { BASE } from '@/lib/api'
import { cn } from '@/lib/utils'

// BrandMark is the agentport mark, served by this Companion like every asset.
export function BrandMark({ className }: { className?: string }) {
  return <img src={BASE + '/assets/agentport-mark.svg'} alt="" width={28} height={28} className={cn('size-7', className)} />
}

export function GitHubIcon(props: SVGProps<SVGSVGElement>) {
  return (
    <svg viewBox="0 0 16 16" aria-hidden="true" fill="currentColor" {...props}>
      <path d="M8 .2a8 8 0 0 0-2.5 15.6c.4 0 .5-.2.5-.4v-1.5c-2.2.5-2.7-1-2.7-1-.4-.9-.9-1.2-.9-1.2-.7-.5.1-.5.1-.5.8.1 1.2.8 1.2.8.7 1.3 1.9.9 2.4.7 0-.5.3-.9.5-1.1-1.8-.2-3.6-.9-3.6-4 0-.9.3-1.6.8-2.1-.1-.2-.4-1 .1-2.1 0 0 .7-.2 2.2.8a7.6 7.6 0 0 1 4 0c1.5-1 2.2-.8 2.2-.8.4 1.1.2 1.9.1 2.1.5.6.8 1.3.8 2.1 0 3.1-1.9 3.8-3.6 4 .3.3.6.8.6 1.5v2.2c0 .2.1.5.6.4A8 8 0 0 0 8 .2Z" />
    </svg>
  )
}

export function StatusDot({ ok, className }: { ok: boolean; className?: string }) {
  return (
    <span className={cn('relative inline-flex size-2 shrink-0', className)} aria-hidden="true">
      {ok && <span className="absolute inline-flex size-full rounded-full bg-emerald-500/60 motion-safe:animate-ping [animation-duration:2.4s]" />}
      <span className={cn('relative inline-flex size-2 rounded-full', ok ? 'bg-emerald-500' : 'bg-red-500')} />
    </span>
  )
}
