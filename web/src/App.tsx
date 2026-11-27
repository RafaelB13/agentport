import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { Activity, ArrowLeftRight, History, Radar, SquareTerminal } from 'lucide-react'
import { ConnectionAlert } from '@/components/connection-alert'
import { AppHeader } from '@/components/header/app-header'
import { ActivityLog } from '@/components/ports/activity-log'
import { ClosedPorts } from '@/components/ports/closed-ports'
import { DetectedPorts } from '@/components/ports/detected-ports'
import { OpenPorts } from '@/components/ports/open-ports'
import { PendingRequests } from '@/components/requests/pending-requests'
import { SpotlightProvider } from '@/components/spotlight/spotlight-provider'
import { PortField } from '@/components/spotlight/triggers'
import { StatusBar } from '@/components/status-bar'
import { TerminalPanel } from '@/components/terminal/terminal-panel'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useDialogs } from '@/lib/dialogs'
import { useI18n } from '@/lib/i18n'
import { isTyping } from '@/lib/keys'
import { usePanel } from '@/lib/panel'
import type { Tab } from '@/lib/spotlight'

const TABS: Tab[] = ['open', 'det', 'closed', 'log', 'term']

function Count({ n }: { n: number }) {
  return (
    <span className="ml-1.5 rounded-md bg-muted-foreground/10 px-1.5 py-px text-xs font-semibold tabular-nums text-muted-foreground group-data-[state=active]:bg-primary/10 group-data-[state=active]:text-foreground">
      {n}
    </span>
  )
}

export default function App() {
  const { t } = useI18n()
  const panel = usePanel()
  const dialogs = useDialogs()
  const [tab, setTabState] = useState<Tab>('open')
  const [terminalRequest, setTerminalRequest] = useState(0)

  function setTab(next: Tab) {
    setTabState(next)
    if (next === 'det') void panel.loadDetected(true)
  }

  // Keyboard: ? lists the shortcuts, 1 to 4 pick a tab. A and R (approve and
  // deny the oldest request) live with the requests; / and Cmd+K (the search)
  // with the search.
  const setTabRef = useRef(setTab)
  setTabRef.current = setTab
  const goTab = useCallback((next: Tab) => setTabRef.current(next), [])
  const openTerminal = useCallback(() => {
    setTabRef.current('term')
    setTerminalRequest((n) => n + 1)
  }, [])
  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      if (isTyping(event.target) || event.ctrlKey || event.metaKey || event.altKey || dialogs.anyOpen) return
      if (event.key === '?') {
        dialogs.openShortcuts()
      } else if (event.key >= '1' && event.key <= '4') {
        setTabRef.current(TABS[Number(event.key) - 1])
      }
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [dialogs])

  const triggers: [Tab, ReactNode, string, number | null][] = [
    ['open', <ArrowLeftRight key="i" />, t('Open'), panel.forwards.length],
    ['det', <Radar key="i" />, t('Detected on server'), panel.detected.length],
    ['closed', <History key="i" />, t('Closed'), panel.closed.length],
    ['log', <Activity key="i" />, t('Activity'), null],
    ['term', <SquareTerminal key="i" />, t('Terminal'), null],
  ]

  return (
    <SpotlightProvider onTab={goTab} openTerminal={openTerminal}>
    <div className="flex h-dvh flex-col overflow-hidden">
      <AppHeader />
      <main className="flex min-h-0 flex-1 flex-col">
        <div className="mx-auto flex min-h-0 w-full max-w-screen-2xl flex-1 flex-col gap-3 px-4 py-3 sm:px-6 sm:py-4">
          <ConnectionAlert />
          <PendingRequests />
          <Tabs value={tab} onValueChange={(value) => setTab(value as Tab)} className="flex min-h-0 flex-1 flex-col">
            <div className="flex shrink-0 flex-col gap-2 lg:flex-row lg:items-center lg:justify-between lg:gap-4">
              <TabsList className="h-9 w-full justify-start overflow-x-auto lg:w-auto [scrollbar-width:none]">
                {triggers.map(([value, icon, label, count]) => (
                  <TabsTrigger
                    key={value}
                    value={value}
                    className="group shrink-0 gap-1.5 px-2.5 max-sm:flex-1 [&_svg]:size-4 [&_svg]:shrink-0"
                    aria-label={label}
                  >
                    {icon}
                    <span className="max-sm:sr-only">{label}</span>
                    {count !== null && <Count n={count} />}
                  </TabsTrigger>
                ))}
              </TabsList>
              <PortField className="lg:w-80 xl:w-96" />
            </div>
            <TabsContent value="open" forceMount className="mt-3 min-h-0 flex-1 data-[state=inactive]:hidden">
              <OpenPorts />
            </TabsContent>
            <TabsContent value="det" forceMount className="mt-3 min-h-0 flex-1 data-[state=inactive]:hidden">
              <DetectedPorts />
            </TabsContent>
            <TabsContent value="closed" forceMount className="mt-3 min-h-0 flex-1 data-[state=inactive]:hidden">
              <ClosedPorts />
            </TabsContent>
            <TabsContent value="log" forceMount className="mt-3 min-h-0 flex-1 data-[state=inactive]:hidden">
              <ActivityLog />
            </TabsContent>
            <TabsContent value="term" forceMount className="mt-3 min-h-0 flex-1 data-[state=inactive]:hidden">
              <TerminalPanel active={tab === 'term'} openRequest={terminalRequest} />
            </TabsContent>
          </Tabs>
        </div>
      </main>
      <StatusBar />
    </div>
    </SpotlightProvider>
  )
}
