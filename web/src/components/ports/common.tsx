import type { ReactNode } from 'react'
import { ArrowRight } from 'lucide-react'
import { Card } from '@/components/ui/card'
import { cn } from '@/lib/utils'

// PortRoute shows where a forward goes: the server port to the local address.
export function PortRoute({ remote, local, className }: { remote: number; local?: number; className?: string }) {
  return (
    <span className={cn('flex min-w-0 items-center gap-1.5 font-mono text-sm tabular-nums', className)}>
      <span className="font-semibold text-foreground">:{remote}</span>
      {local !== undefined && (
        <>
          <ArrowRight className="size-3 shrink-0 text-muted-foreground/70" aria-hidden="true" />
          <span className="truncate text-muted-foreground">localhost:{local}</span>
        </>
      )}
    </span>
  )
}

export function EmptyState({ icon, children }: { icon?: ReactNode; children: ReactNode }) {
  return (
    <div className="flex h-full min-h-48 flex-col items-center justify-center gap-3 px-6 py-12 text-center text-sm text-muted-foreground">
      {icon && <div className="flex size-11 items-center justify-center rounded-full border bg-muted/40 [&_svg]:size-5">{icon}</div>}
      <div className="max-w-md leading-relaxed">{children}</div>
    </div>
  )
}

// DetailList is a compact grid of labelled values.
export function DetailList({ items, className }: { items: [string, ReactNode][]; className?: string }) {
  return (
    <dl className={cn('grid grid-cols-1 gap-x-6 gap-y-2.5 sm:grid-cols-2 lg:grid-cols-4', className)}>
      {items.map(([label, value]) => (
        <div key={label} className="min-w-0">
          <dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{label}</dt>
          <dd className="mt-0.5 break-words font-mono text-xs text-foreground">{value}</dd>
        </div>
      ))}
    </dl>
  )
}

export function ListCard({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <Card className={cn('flex h-full min-h-0 flex-col overflow-hidden rounded-lg shadow-sm', className)}>{children}</Card>
  )
}
