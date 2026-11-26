import { BrandMark } from '@/components/brand'
import { Separator } from '@/components/ui/separator'
import { HeaderActions } from './header-actions'
import { ServerSwitcher } from './server-switcher'

// AppHeader is one row: the brand, the server switcher, and the actions on the
// right (icon buttons on wide screens, one overflow menu below 900px).
export function AppHeader() {
  return (
    <header className="shrink-0 border-b bg-background">
      <div className="mx-auto flex h-14 w-full max-w-screen-2xl items-center gap-2 px-4 sm:gap-3 sm:px-6">
        <div className="flex shrink-0 items-center gap-2">
          <BrandMark />
          <span className="hidden text-[15px] font-semibold tracking-tight sm:inline">agentport</span>
        </div>
        <Separator orientation="vertical" className="h-5" />
        <div className="flex min-w-0 flex-1 items-center">
          <ServerSwitcher />
        </div>
        <div className="flex shrink-0 items-center">
          <HeaderActions />
        </div>
      </div>
    </header>
  )
}
