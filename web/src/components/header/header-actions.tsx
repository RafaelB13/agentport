import type { ComponentPropsWithoutRef, ReactNode } from 'react'
import {
  Bell,
  BellOff,
  CircleHelp,
  Ellipsis,
  ExternalLink,
  Info,
  Keyboard,
  Languages,
  Lock,
  Moon,
  Play,
  Plug,
  Power,
  ShieldCheck,
  Sun,
  SunMoon,
  TriangleAlert,
} from 'lucide-react'
import { GitHubIcon } from '@/components/brand'
import { Tip } from '@/components/tip'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuPortal,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Separator } from '@/components/ui/separator'
import { useDialogs, type InfoTab } from '@/lib/dialogs'
import { LANG_NAMES, LANG_SHORT, useI18n } from '@/lib/i18n'
import { LANGS, type Lang } from '@/lib/language.js'
import { ISSUES_URL, PROJECT_LABEL, PROJECT_URL } from '@/lib/links'
import { usePanel } from '@/lib/panel'
import { usePolicy, type Policy } from '@/lib/policy'
import { useStopArming } from '@/lib/stop'
import { themeLabel as labelOfTheme, useTheme, type ThemeChoice } from '@/lib/theme'
import { cn } from '@/lib/utils'

const THEME_ICONS: Record<ThemeChoice, typeof Sun> = { system: SunMoon, light: Sun, dark: Moon }

function useThemeLabel() {
  const { t } = useI18n()
  return (choice: ThemeChoice) => labelOfTheme(t, choice)
}

function PolicyItems({ policy, setPolicy }: { policy: Policy; setPolicy: (policy: Policy) => void }) {
  const { t } = useI18n()
  const panel = usePanel()
  return (
    <>
      <DropdownMenuLabel className="text-xs font-medium text-muted-foreground">{t('When to ask for approval')}</DropdownMenuLabel>
      <DropdownMenuRadioGroup value={policy} onValueChange={(value) => setPolicy(value as Policy)}>
        <DropdownMenuRadioItem value="ask" className="items-start py-2">
          <MenuText title={t('Ask every time')} hint={t('Each port the agent asks for waits for your approval.')} />
        </DropdownMenuRadioItem>
        <DropdownMenuRadioItem value="session" className="items-start py-2">
          <MenuText
            title={t('Remember for this session')}
            hint={t('A port you already approved reopens without asking until the session ends.')}
          />
        </DropdownMenuRadioItem>
      </DropdownMenuRadioGroup>
      <DropdownMenuSeparator />
      <DropdownMenuCheckboxItem
        className="items-start py-2"
        checked={panel.settings.auto_open}
        onCheckedChange={(checked) => void panel.saveSettings({ auto_open: checked === true })}
        onSelect={(event) => event.preventDefault()}
      >
        <MenuText
          title={t('Open the panel automatically')}
          hint={t('When an agent asks and no panel is open, open it in the browser.')}
        />
      </DropdownMenuCheckboxItem>
    </>
  )
}

function MenuText({ title, hint }: { title: string; hint: string }) {
  return (
    <span className="flex min-w-0 flex-col gap-0.5">
      <span className="font-semibold">{title}</span>
      <span className="text-xs leading-snug text-muted-foreground">{hint}</span>
    </span>
  )
}

function HelpItems() {
  const { t } = useI18n()
  const { status } = usePanel()
  const dialogs = useDialogs()
  const topics: [InfoTab, typeof Info, string, string][] = [
    ['about', Info, t('What agentport is'), t('How it works, in three steps')],
    ['start', Play, t('First steps'), t('Install, connect and use with your agent')],
    ['mcp', Plug, t('MCP tools'), t('What the agent can and cannot do')],
    ['sec', Lock, t('Security and privacy'), t('Where ports listen and what leaves the machine')],
    ['trouble', TriangleAlert, t('Troubleshooting'), t('Busy port, dropped tunnel, agent without MCP')],
  ]
  return (
    <>
      <DropdownMenuGroup>
        {topics.map(([tab, Icon, title, hint]) => (
          <DropdownMenuItem key={tab} className="items-start gap-2.5 py-2" onSelect={() => dialogs.openInfo(tab)}>
            <Icon className="mt-0.5 size-4 text-muted-foreground" />
            <MenuText title={title} hint={hint} />
          </DropdownMenuItem>
        ))}
        <DropdownMenuItem className="gap-2.5" onSelect={() => dialogs.openShortcuts()}>
          <Keyboard className="size-4 text-muted-foreground" />
          <span className="font-semibold">{t('Keyboard shortcuts')}</span>
          <DropdownMenuShortcut>?</DropdownMenuShortcut>
        </DropdownMenuItem>
      </DropdownMenuGroup>
      <DropdownMenuSeparator />
      <DropdownMenuItem asChild className="items-start gap-2.5 py-2">
        <a href={PROJECT_URL} target="_blank" rel="noopener noreferrer">
          <ExternalLink className="mt-0.5 size-4 text-muted-foreground" />
          <MenuText title={t('Project on GitHub')} hint={PROJECT_LABEL} />
        </a>
      </DropdownMenuItem>
      <DropdownMenuItem asChild className="gap-2.5">
        <a href={ISSUES_URL} target="_blank" rel="noopener noreferrer">
          <ExternalLink className="size-4 text-muted-foreground" />
          <span className="font-semibold">{t('Report a problem')}</span>
        </a>
      </DropdownMenuItem>
      <DropdownMenuSeparator />
      <p className="px-2 py-1 text-xs text-muted-foreground">agentport{status?.version ? ' ' + status.version : ''}</p>
    </>
  )
}

function IconButton({
  label,
  tip,
  children,
  className,
  ...props
}: {
  label: string
  tip?: ReactNode
  children: ReactNode
  className?: string
} & ComponentPropsWithoutRef<typeof Button>) {
  return (
    <Tip label={tip ?? label}>
      <Button variant="ghost" size="icon" className={cn('size-8 text-muted-foreground', className)} aria-label={label} {...props}>
        {children}
      </Button>
    </Tip>
  )
}

export function HeaderActions() {
  const { t, lang, setLang } = useI18n()
  const panel = usePanel()
  const theme = useTheme()
  const themeLabel = useThemeLabel()
  const { policy, setPolicy } = usePolicy()
  const stop = useStopArming()
  const ThemeIcon = THEME_ICONS[theme.choice]
  const policyLabel = policy === 'session' ? t('Remember for this session') : t('Ask every time')
  const notify = panel.settings.notify

  return (
    <>
      {/* Wide screens: compact icon buttons with tooltips. */}
      <div className="hidden items-center gap-0.5 min-[900px]:flex">
        <DropdownMenu>
          <Tip
            label={
              <span className="flex flex-col gap-0.5">
                <span className="font-semibold">{t('When to ask for approval')}</span>
                <span className="text-muted-foreground">{policyLabel}</span>
              </span>
            }
          >
            <DropdownMenuTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                className="size-8 text-muted-foreground data-[state=open]:bg-accent"
                aria-label={t('When to ask for approval')}
              >
                <ShieldCheck />
              </Button>
            </DropdownMenuTrigger>
          </Tip>
          <DropdownMenuContent align="end" className="w-80">
            <PolicyItems policy={policy} setPolicy={setPolicy} />
          </DropdownMenuContent>
        </DropdownMenu>

        <IconButton
          label={t('Notifications')}
          tip={t('Notify me when an agent asks for a port')}
          aria-pressed={notify}
          className={cn(notify && 'text-foreground')}
          onClick={() => void panel.saveSettings({ notify: !notify })}
        >
          {notify ? <Bell /> : <BellOff />}
        </IconButton>

        <DropdownMenu>
          <Tip label={t('Language')}>
            <DropdownMenuTrigger asChild>
              <Button
                variant="ghost"
                className="h-8 gap-1.5 px-2 text-xs font-semibold text-muted-foreground data-[state=open]:bg-accent"
                aria-label={t('Language')}
              >
                <Languages className="size-4" />
                {LANG_SHORT[lang]}
              </Button>
            </DropdownMenuTrigger>
          </Tip>
          <DropdownMenuContent align="end" className="w-52">
            <DropdownMenuRadioGroup value={lang} onValueChange={(value) => setLang(value as Lang)}>
              {LANGS.map((code) => (
                <DropdownMenuRadioItem key={code} value={code}>
                  {LANG_NAMES[code]}
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuContent>
        </DropdownMenu>

        <IconButton
          label={t('Change theme')}
          tip={t('Theme: {theme}', { theme: themeLabel(theme.choice) })}
          onClick={theme.cycle}
        >
          <ThemeIcon />
        </IconButton>

        <DropdownMenu>
          <Tip label={t('Help and information')}>
            <DropdownMenuTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                className="size-8 text-muted-foreground data-[state=open]:bg-accent"
                aria-label={t('Help')}
              >
                <CircleHelp />
              </Button>
            </DropdownMenuTrigger>
          </Tip>
          <DropdownMenuContent align="end" className="w-80">
            <HelpItems />
          </DropdownMenuContent>
        </DropdownMenu>

        <Tip label={PROJECT_LABEL}>
          <Button asChild variant="ghost" size="icon" className="size-8 text-muted-foreground">
            <a href={PROJECT_URL} target="_blank" rel="noopener noreferrer" aria-label="GitHub">
              <GitHubIcon className="size-4" />
            </a>
          </Button>
        </Tip>

        <Separator orientation="vertical" className="mx-1.5 h-5" />

        <IconButton
          label={t('Stop the Companion')}
          tip={stop.armed ? t('Click again to stop the Companion') : t('Stop the Companion (closes every port)')}
          className={cn(
            'hover:bg-destructive/10 hover:text-destructive',
            stop.armed && 'bg-destructive text-destructive-foreground hover:bg-destructive/90 hover:text-destructive-foreground',
          )}
          onClick={() => stop.press()}
        >
          <Power />
        </IconButton>
      </div>

      {/* Narrow screens: every secondary action in one menu. */}
      <div className="min-[900px]:hidden">
        <DropdownMenu>
          <Tip label={t('More actions')}>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="icon" className="size-8 data-[state=open]:bg-accent" aria-label={t('More actions')}>
                <Ellipsis />
              </Button>
            </DropdownMenuTrigger>
          </Tip>
          <DropdownMenuContent align="end" className="w-72">
            <PolicyItems policy={policy} setPolicy={setPolicy} />
            <DropdownMenuSeparator />
            <DropdownMenuCheckboxItem
              checked={notify}
              onCheckedChange={(checked) => void panel.saveSettings({ notify: checked === true })}
              onSelect={(event) => event.preventDefault()}
            >
              {t('Notifications')}
            </DropdownMenuCheckboxItem>
            <DropdownMenuSub>
              <DropdownMenuSubTrigger className="gap-2">
                <Languages className="size-4 text-muted-foreground" />
                {t('Language')}
                <span className="ml-auto text-xs text-muted-foreground">{LANG_SHORT[lang]}</span>
              </DropdownMenuSubTrigger>
              <DropdownMenuPortal>
                <DropdownMenuSubContent>
                  <DropdownMenuRadioGroup value={lang} onValueChange={(value) => setLang(value as Lang)}>
                    {LANGS.map((code) => (
                      <DropdownMenuRadioItem key={code} value={code}>
                        {LANG_NAMES[code]}
                      </DropdownMenuRadioItem>
                    ))}
                  </DropdownMenuRadioGroup>
                </DropdownMenuSubContent>
              </DropdownMenuPortal>
            </DropdownMenuSub>
            <DropdownMenuSub>
              <DropdownMenuSubTrigger className="gap-2">
                <ThemeIcon className="size-4 text-muted-foreground" />
                {t('Theme')}
              </DropdownMenuSubTrigger>
              <DropdownMenuPortal>
                <DropdownMenuSubContent>
                  <DropdownMenuRadioGroup value={theme.choice} onValueChange={(value) => theme.setChoice(value as ThemeChoice)}>
                    {(['system', 'light', 'dark'] as ThemeChoice[]).map((choice) => (
                      <DropdownMenuRadioItem key={choice} value={choice} className="capitalize">
                        {themeLabel(choice)}
                      </DropdownMenuRadioItem>
                    ))}
                  </DropdownMenuRadioGroup>
                </DropdownMenuSubContent>
              </DropdownMenuPortal>
            </DropdownMenuSub>
            <DropdownMenuSub>
              <DropdownMenuSubTrigger className="gap-2">
                <CircleHelp className="size-4 text-muted-foreground" />
                {t('Help')}
              </DropdownMenuSubTrigger>
              <DropdownMenuPortal>
                <DropdownMenuSubContent className="w-72">
                  <HelpItems />
                </DropdownMenuSubContent>
              </DropdownMenuPortal>
            </DropdownMenuSub>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              className="gap-2 text-destructive focus:bg-destructive/10 focus:text-destructive"
              onSelect={(event) => {
                if (!stop.press()) event.preventDefault()
              }}
            >
              <Power className="size-4" />
              {stop.armed ? t('Click again to stop the Companion') : t('Stop the Companion')}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </>
  )
}
