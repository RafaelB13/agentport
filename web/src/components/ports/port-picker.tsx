import { forwardRef, useState } from 'react'
import { Plus } from 'lucide-react'
import { Command, CommandGroup, CommandInput, CommandItem, CommandList } from '@/components/ui/command'
import { useI18n } from '@/lib/i18n'
import { usePanel } from '@/lib/panel'
import { cn } from '@/lib/utils'

// PortPicker opens a port: pick one of the server's listening ports, or type a
// number. It scans the server again whenever it opens.
export const PortPicker = forwardRef<HTMLInputElement, { className?: string }>(function PortPicker({ className }, ref) {
  const { t } = useI18n()
  const panel = usePanel()
  const [query, setQuery] = useState('')
  const [open, setOpen] = useState(false)
  const typed = query.trim()
  const number = Number(typed)
  const valid = /^\d+$/.test(typed) && number >= 1 && number <= 65535
  const matches = panel.detected.filter((p) => !typed || String(p.port).startsWith(typed))
  const exact = matches.some((p) => String(p.port) === typed)

  async function submit(port: number, input: HTMLInputElement | null) {
    setOpen(false)
    if (await panel.openPort(port)) {
      setQuery('')
      input?.blur()
    }
  }

  function show() {
    if (!open) void panel.loadDetected(true)
    setOpen(true)
  }

  return (
    <Command
      shouldFilter={false}
      loop
      className={cn('relative h-auto w-full overflow-visible bg-transparent', className)}
      onKeyDown={(event) => {
        if (event.key !== 'Escape') return
        if (open) {
          event.stopPropagation()
          setOpen(false)
        } else {
          ;(event.target as HTMLElement).blur()
        }
      }}
    >
      <CommandInput
        ref={ref}
        value={query}
        onValueChange={(value) => {
          setQuery(value)
          show()
        }}
        onFocus={show}
        onBlur={() => setOpen(false)}
        inputMode="numeric"
        aria-label={t('Open a port')}
        placeholder={t('Open a port: pick or type…')}
        wrapperClassName="h-9 rounded-md border border-input bg-background px-3 shadow-sm transition-shadow focus-within:ring-1 focus-within:ring-ring"
        className="h-9 py-0"
        icon={<Plus className="mr-2 size-4 shrink-0 text-muted-foreground" aria-hidden="true" />}
        suffix={
          <kbd className="pointer-events-none ml-2 hidden h-5 select-none items-center rounded border bg-muted px-1.5 font-mono text-[10px] font-medium text-muted-foreground sm:inline-flex">
            /
          </kbd>
        }
      />
      {open && (
        <div
          className="absolute right-0 top-full z-50 mt-1.5 w-full min-w-[18rem] overflow-hidden rounded-md border bg-popover text-popover-foreground shadow-md animate-in fade-in-0 zoom-in-95"
          onMouseDown={(event) => event.preventDefault()}
        >
          <CommandList className="max-h-80" aria-label={t('Ports on the server')}>
            {typed && !exact && (
              <CommandGroup>
                <CommandItem
                  value={'typed:' + typed}
                  disabled={!valid}
                  onSelect={() => void submit(number, document.activeElement as HTMLInputElement)}
                >
                  {valid ? (
                    <>
                      <Plus />
                      {t('Open port')}
                      <span className="font-mono font-semibold">:{typed}</span>
                    </>
                  ) : (
                    <span className="text-destructive">{t('Use a number between 1 and 65535.')}</span>
                  )}
                </CommandItem>
              </CommandGroup>
            )}
            {matches.length > 0 ? (
              <CommandGroup heading={t('Ports on the server')}>
                {matches.map((p) => (
                  <CommandItem
                    key={p.port}
                    value={String(p.port)}
                    onSelect={() => void submit(p.port, document.activeElement as HTMLInputElement)}
                  >
                    <span className="w-14 shrink-0 font-mono text-[13px] font-semibold tabular-nums">:{p.port}</span>
                    <span className="min-w-0 truncate">{p.process || t('Unknown process')}</span>
                    <span className="ml-auto shrink-0 font-mono text-xs text-muted-foreground">{p.bind}</span>
                  </CommandItem>
                ))}
              </CommandGroup>
            ) : (
              !typed && (
                <p className="px-3 py-5 text-center text-xs text-muted-foreground">
                  {panel.detectedBusy ? t('Scanning the server…') : t('No other listening ports were found on the server.')}
                </p>
              )
            )}
          </CommandList>
        </div>
      )}
    </Command>
  )
})
