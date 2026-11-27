import { useEffect, useRef, useState, type ReactNode } from 'react'
import { ChevronDown, EllipsisVertical, ExternalLink, Pencil, Pin, X } from 'lucide-react'
import { Tip } from '@/components/tip'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { remoteProcessName } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import { usePanel } from '@/lib/panel'
import type { Forward } from '@/lib/types'
import { cn } from '@/lib/utils'
import { PortRoute } from './common'
import { ForwardDetails } from './details'

export interface ForwardRowProps {
  forward: Forward
  selected: boolean
  expanded: boolean
  renaming: boolean
  fresh: boolean
  onSelect: (selected: boolean) => void
  onToggleDetails: () => void
  onStartRename: () => void
  onEndRename: () => void
  onEdit: () => void
  onClose: () => void
}

export function ForwardBadges({ forward: f }: { forward: Forward }) {
  const { t } = useI18n()
  const badges: ReactNode[] = []
  if (f.state === 'active') {
    badges.push(
      <Badge key="live" variant="success" className="gap-1">
        <span className="size-1.5 rounded-full bg-current" aria-hidden="true" />
        {t('Live')}
      </Badge>,
    )
  }
  if (f.state === 'reserved') badges.push(<Badge key="wait" variant="warning">{t('Waiting for the tunnel')}</Badge>)
  if (f.state === 'session_ended') badges.push(<Badge key="ended" variant="muted">{t('Agent ended')}</Badge>)
  badges.push(
    f.origin === 'manual' ? (
      <Badge key="origin" variant="outline">
        {t('You')}
      </Badge>
    ) : (
      <Badge key="origin" variant="info">
        {t('Agent')}
      </Badge>
    ),
  )
  for (const owner of f.owners || []) {
    if (owner !== 'user') {
      badges.push(
        <Badge key={'o:' + owner} variant="secondary" className="max-w-40 font-mono">
          <span className="truncate">{owner}</span>
        </Badge>,
      )
    }
  }
  if (f.pinned) {
    badges.push(
      <Badge key="pin" variant="warning" className="gap-1">
        <Pin className="size-3" aria-hidden="true" />
        {t('Pinned')}
      </Badge>,
    )
  }
  return <>{badges}</>
}

export function ForwardRow(props: ForwardRowProps) {
  const { forward: f, selected, expanded, renaming, fresh } = props
  const { t } = useI18n()
  const panel = usePanel()
  const leaving = panel.leaving.has(f.remote_port)
  const name = f.label || remoteProcessName(panel.portInfo.get(f.remote_port)) || t('No name')
  const href = f.url + (f.path || '')

  return (
    <div
      data-port={f.remote_port}
      className={cn(
        'border-b transition-opacity duration-150 last:border-b-0',
        fresh && 'motion-safe:animate-row-in',
        leaving && 'opacity-0',
      )}
    >
      <div
        className={cn(
          'grid items-center gap-x-3 px-3 py-2 transition-colors sm:px-4 md:gap-x-4',
          'grid-cols-[auto_minmax(0,1fr)_auto] md:grid-cols-[auto_13rem_minmax(0,1fr)_auto]',
          selected ? 'bg-muted/60' : 'hover:bg-muted/30',
        )}
        onDoubleClick={(event) => {
          if ((event.target as HTMLElement).closest('button,a,input,[role="checkbox"]')) return
          props.onStartRename()
        }}
      >
        <Checkbox
          checked={selected}
          onCheckedChange={(value) => props.onSelect(value === true)}
          aria-label={t('Select :{port}', { port: f.remote_port })}
        />
        <PortRoute remote={f.remote_port} local={f.local_port} className="hidden md:flex" />
        <div className="min-w-0">
          <PortRoute remote={f.remote_port} local={f.local_port} className="mb-0.5 text-xs md:hidden" />
          <div className="flex min-w-0 items-center gap-2">
            {renaming ? (
              <RenameInput forward={f} onDone={props.onEndRename} />
            ) : (
              <Tip
                side="top"
                label={
                  <span className="flex max-w-80 flex-col gap-0.5">
                    <span className="break-words font-semibold">{name}</span>
                    <span className="text-muted-foreground">{t('Double-click to rename')}</span>
                  </span>
                }
              >
                <span className="min-w-0 truncate text-sm font-semibold" onDoubleClick={props.onStartRename}>
                  {name}
                </span>
              </Tip>
            )}
            <span className="hidden shrink-0 items-center gap-1 lg:flex">
              <ForwardBadges forward={f} />
            </span>
          </div>
          <Tip side="bottom" label={t('Open in a new tab')}>
            <a
              className="block w-fit max-w-full truncate font-mono text-[13px] text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
              href={href}
              target="_blank"
              rel="noopener noreferrer"
            >
              {href}
            </a>
          </Tip>
          <div className="mt-1.5 flex flex-wrap gap-1 lg:hidden">
            <ForwardBadges forward={f} />
          </div>
        </div>
        <RowActions {...props} href={href} />
      </div>
      {expanded && <ForwardDetails forward={f} />}
    </div>
  )
}

function RowActions({ forward: f, expanded, href, onToggleDetails, onEdit, onClose }: ForwardRowProps & { href: string }) {
  const { t } = useI18n()
  const panel = usePanel()
  const pinLabel = f.pinned ? t('Unpin') : t('Pin')
  return (
    <div className="flex items-center gap-0.5">
      <Tip label={t('Open in a new tab')}>
        <Button asChild size="sm" className="h-8 gap-1.5 px-2.5 max-md:size-8 max-md:px-0">
          <a href={href} target="_blank" rel="noopener noreferrer" aria-label={t('Open in a new tab')}>
            <ExternalLink />
            <span className="max-md:hidden">{t('Open link')}</span>
          </a>
        </Button>
      </Tip>
      <div className="hidden items-center gap-0.5 md:flex">
        <Tip label={pinLabel}>
          <Button
            variant="ghost"
            size="icon"
            className={cn('size-8 text-muted-foreground', f.pinned && 'text-amber-600 dark:text-amber-400')}
            aria-label={pinLabel}
            aria-pressed={f.pinned}
            onClick={() => void panel.togglePin(f)}
          >
            <Pin className={cn(f.pinned && 'fill-current')} />
          </Button>
        </Tip>
        <Tip label={t('Edit')}>
          <Button variant="ghost" size="icon" className="size-8 text-muted-foreground" aria-label={t('Edit')} onClick={onEdit}>
            <Pencil />
          </Button>
        </Tip>
        <Tip label={t('Details')}>
          <Button
            variant="ghost"
            size="icon"
            className="size-8 text-muted-foreground"
            aria-label={t('Details')}
            aria-expanded={expanded}
            onClick={onToggleDetails}
          >
            <ChevronDown className={cn('transition-transform duration-200', expanded && 'rotate-180')} />
          </Button>
        </Tip>
        <Tip label={t('Close')}>
          <Button
            variant="ghost"
            size="icon"
            className="size-8 text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
            aria-label={t('Close')}
            onClick={onClose}
          >
            <X />
          </Button>
        </Tip>
      </div>
      <div className="md:hidden">
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon" className="size-8 text-muted-foreground" aria-label={t('More actions')}>
              <EllipsisVertical />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-44">
            <DropdownMenuItem onSelect={() => void panel.togglePin(f)}>
              <Pin />
              {pinLabel}
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={onEdit}>
              <Pencil />
              {t('Edit')}
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={onToggleDetails}>
              <ChevronDown />
              {t('Details')}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem className="text-destructive focus:bg-destructive/10 focus:text-destructive" onSelect={onClose}>
              <X />
              {t('Close')}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>
  )
}

// RenameInput edits a port's name in place: Enter or leaving the field saves,
// Escape cancels.
function RenameInput({ forward: f, onDone }: { forward: Forward; onDone: () => void }) {
  const { t } = useI18n()
  const panel = usePanel()
  const [value, setValue] = useState(f.label || '')
  const ref = useRef<HTMLInputElement>(null)
  const done = useRef(false)

  useEffect(() => {
    ref.current?.focus()
    ref.current?.select()
  }, [])

  function commit() {
    if (done.current) return
    done.current = true
    onDone()
    void panel.rename(f, value)
  }

  return (
    <Input
      ref={ref}
      value={value}
      maxLength={64}
      className="h-7 max-w-80 px-2 text-sm"
      aria-label={t('Rename :{port}', { port: f.remote_port })}
      onChange={(event) => setValue(event.target.value)}
      onKeyDown={(event) => {
        if (event.key === 'Enter') commit()
        if (event.key === 'Escape') {
          event.stopPropagation()
          done.current = true
          onDone()
        }
      }}
      onBlur={commit}
    />
  )
}
