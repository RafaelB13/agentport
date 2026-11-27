import { Search } from 'lucide-react'
import { Tip } from '@/components/tip'
import { Button } from '@/components/ui/button'
import { useI18n } from '@/lib/i18n'
import { MOD_KEY, useSpotlight } from '@/lib/spotlight'
import { cn } from '@/lib/utils'
import { Keys } from './parts'

// SearchButton opens the search from the header: a wide button with the
// shortcut from 1024px, a magnifier below that.
export function SearchButton() {
  const { t } = useI18n()
  const spotlight = useSpotlight()
  return (
    <>
      <Button
        variant="outline"
        className="hidden h-8 w-56 justify-start gap-2 rounded-lg border-input bg-muted/40 px-2.5 text-[13px] font-medium text-muted-foreground shadow-none hover:bg-muted hover:text-foreground lg:flex xl:w-64"
        aria-label={t('Search ports, servers and commands')}
        onClick={() => spotlight.open()}
      >
        <Search className="size-4" strokeWidth={2.25} />
        <span className="flex-1 truncate text-left">{t('Search')}</span>
        <Keys keys={[MOD_KEY, 'K']} />
      </Button>
      <Tip label={t('Search ports, servers and commands')}>
        <Button
          variant="ghost"
          size="icon"
          className="size-8 text-muted-foreground lg:hidden"
          aria-label={t('Search ports, servers and commands')}
          onClick={() => spotlight.open()}
        >
          <Search />
        </Button>
      </Tip>
    </>
  )
}

// PortField sits where the port picker was, in the tabs row. It looks like a
// field and opens the search: a click opens it empty, and a key typed while it
// has the focus opens it with that key typed in.
export function PortField({ className }: { className?: string }) {
  const { t } = useI18n()
  const spotlight = useSpotlight()
  return (
    <button
      type="button"
      aria-label={t('Open a port')}
      aria-haspopup="dialog"
      className={cn(
        'flex h-9 w-full items-center rounded-md border border-input bg-background px-3 text-left text-sm shadow-sm transition-shadow hover:border-ring/40 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring',
        className,
      )}
      onClick={() => spotlight.open()}
      onKeyDown={(event) => {
        // Space presses the button, and / opens the search through its shortcut.
        if (event.key.length !== 1 || event.key === ' ' || event.key === '/' || event.metaKey || event.ctrlKey || event.altKey) return
        event.preventDefault()
        spotlight.open(event.key)
      }}
    >
      <Search className="mr-2 size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
      <span className="min-w-0 flex-1 truncate text-muted-foreground">{t('Open a port: pick or type…')}</span>
      <Keys keys={['/']} className="ml-2 max-sm:hidden" />
    </button>
  )
}
