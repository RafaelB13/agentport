import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App'
import { DialogsProvider } from './components/dialogs/dialogs-provider'
import { I18nProvider } from './components/providers/i18n-provider'
import { PanelProvider } from './components/providers/panel-provider'
import { ThemeProvider } from './components/providers/theme-provider'
import { Toaster } from './components/ui/sonner'
import { TooltipProvider } from './components/ui/tooltip'
// The UI fonts, bundled with the panel: the CSP allows no other origin.
import '@fontsource-variable/inter'
import '@fontsource-variable/jetbrains-mono'
import './index.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ThemeProvider>
      <I18nProvider>
        <TooltipProvider delayDuration={250} skipDelayDuration={100}>
          <PanelProvider>
            <DialogsProvider>
              <App />
            </DialogsProvider>
          </PanelProvider>
          <Toaster />
        </TooltipProvider>
      </I18nProvider>
    </ThemeProvider>
  </StrictMode>,
)
