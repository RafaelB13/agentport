import { ExternalLink } from 'lucide-react'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import type { InfoTab } from '@/lib/dialogs'
import { useI18n } from '@/lib/i18n'
import { PROJECT_LABEL, PROJECT_URL } from '@/lib/links'
import { usePanel } from '@/lib/panel'

type Block = ['p', string] | ['ol' | 'ul', string[]]

export function InfoDialog({
  tab,
  onTab,
  onClose,
}: {
  tab: InfoTab | null
  onTab: (tab: InfoTab) => void
  onClose: () => void
}) {
  const { t } = useI18n()
  const { status } = usePanel()
  const tabs: [InfoTab, string, Block[]][] = [
    [
      'about',
      t('What it is'),
      [
        ['p', t('agentport lets an AI agent open a port of a server on your computer, so you can see the service it runs.')],
        [
          'ol',
          [
            t('The agent asks for a port.'),
            t('You approve it here, or let it open.'),
            t('The port appears on localhost, and you close it when you are done.'),
          ],
        ],
      ],
    ],
    [
      'start',
      t('First steps'),
      [
        ['p', t('Set up a server once in a terminal with agentport setup. Then ask your agent to open a port, for example the database.')],
        [
          'ol',
          [
            t('Install agentport on this computer.'),
            t('Connect a server with agentport setup.'),
            t('Approve the first request that appears here.'),
          ],
        ],
      ],
    ],
    [
      'mcp',
      t('MCP tools'),
      [
        ['p', t('The agent can ask for ports, list the ports it holds and release them. It cannot open ports you did not approve.')],
        [
          'ul',
          [
            t('It can ask for a port by number.'),
            t('It can release a port it opened.'),
            t('It cannot change the server or run commands on it.'),
          ],
        ],
      ],
    ],
    [
      'sec',
      t('Security and privacy'),
      [
        [
          'ul',
          [
            t('Ports open only on 127.0.0.1, never on the network.'),
            t('The destination is always 127.0.0.1 of the server, so the agent reaches nothing else.'),
            t('This panel is reachable only from this computer, with a private link.'),
          ],
        ],
      ],
    ],
    [
      'trouble',
      t('Troubleshooting'),
      [
        [
          'ul',
          [
            t('Busy port: pick another free port, or stop the program that holds it when agentport can stop it.'),
            t('Dropped tunnel: the Companion reconnects by itself; the status shows it.'),
            t('The agent does not see the tools: check the MCP setup with agentport doctor.'),
          ],
        ],
      ],
    ],
  ]

  return (
    <Dialog open={tab !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t('About agentport')}</DialogTitle>
          <DialogDescription className="sr-only">{t('How it works, in three steps')}</DialogDescription>
        </DialogHeader>
        <Tabs value={tab ?? 'about'} onValueChange={(value) => onTab(value as InfoTab)}>
          <TabsList className="h-auto w-full flex-wrap justify-start">
            {tabs.map(([key, label]) => (
              <TabsTrigger key={key} value={key} className="text-xs sm:text-sm">
                {label}
              </TabsTrigger>
            ))}
          </TabsList>
          {tabs.map(([key, , blocks]) => (
            <TabsContent key={key} value={key} className="mt-4 min-h-40 space-y-3 text-sm leading-relaxed">
              {blocks.map(([kind, content], index) =>
                kind === 'p' ? (
                  <p key={index}>{content}</p>
                ) : kind === 'ol' ? (
                  <ol key={index} className="list-decimal space-y-1.5 pl-5 text-muted-foreground marker:text-foreground">
                    {content.map((item) => (
                      <li key={item}>{item}</li>
                    ))}
                  </ol>
                ) : (
                  <ul key={index} className="list-disc space-y-1.5 pl-5 text-muted-foreground marker:text-foreground">
                    {content.map((item) => (
                      <li key={item}>{item}</li>
                    ))}
                  </ul>
                ),
              )}
            </TabsContent>
          ))}
        </Tabs>
        <DialogFooter className="flex-row items-center justify-between border-t pt-4 text-xs text-muted-foreground sm:justify-between">
          <span>
            {t('agentport panel')}
            {status?.version ? ' · ' + status.version : ''}
          </span>
          <a
            className="inline-flex items-center gap-1 hover:text-foreground hover:underline"
            href={PROJECT_URL}
            target="_blank"
            rel="noopener noreferrer"
          >
            {PROJECT_LABEL}
            <ExternalLink className="size-3" aria-hidden="true" />
          </a>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function ShortcutsDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useI18n()
  const rows: [string[], string][] = [
    [['/'], t('Open a port (ports on the server)')],
    [['A'], t('Approve the oldest request')],
    [['R'], t('Deny the oldest request')],
    [['1', '4'], t('Open, Detected, Closed, Activity')],
    [['?'], t('Show this list')],
    [['Esc'], t('Close menus and this window')],
  ]
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t('Keyboard shortcuts')}</DialogTitle>
          <DialogDescription>{t('Double-click a port\'s name to rename it.')}</DialogDescription>
        </DialogHeader>
        <dl className="grid grid-cols-[auto_1fr] items-center gap-x-4 gap-y-2.5 text-sm">
          {rows.map(([keys, text]) => (
            <div key={text} className="contents">
              <dt className="flex items-center gap-1">
                {keys.map((key, index) => (
                  <span key={key} className="flex items-center gap-1">
                    {index > 0 && <span className="text-muted-foreground">…</span>}
                    <kbd className="min-w-6 rounded border bg-muted px-1.5 py-0.5 text-center font-mono text-xs">{key}</kbd>
                  </span>
                ))}
              </dt>
              <dd className="text-muted-foreground">{text}</dd>
            </div>
          ))}
        </dl>
      </DialogContent>
    </Dialog>
  )
}

export function ConnectDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useI18n()
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t('Connect a server')}</DialogTitle>
          <DialogDescription className="leading-relaxed">
            {t('Connect a server from this computer in a terminal with agentport setup. It appears in the Switch menu when it is running.')}
          </DialogDescription>
        </DialogHeader>
        <pre className="rounded-md border bg-muted px-3 py-2 font-mono text-sm">agentport setup</pre>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('Close')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
