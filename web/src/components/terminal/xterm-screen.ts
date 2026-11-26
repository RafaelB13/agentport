// The server terminal's screen: xterm.js with the fit and WebGL addons, bundled
// with the panel and loaded only when the Terminal tab opens one.
import { FitAddon } from '@xterm/addon-fit'
import { WebglAddon } from '@xterm/addon-webgl'
import { Terminal, type ITheme } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'

export interface Screen {
  term: Terminal
  fit: () => boolean
  setTheme: (theme: ITheme) => void
  dispose: () => void
}

export function createScreen(container: HTMLElement, theme: ITheme, onData: (data: string) => void): Screen {
  const term = new Terminal({
    cursorBlink: true,
    fontSize: 13,
    scrollback: 5000,
    theme,
    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
  })
  const fit = new FitAddon()
  term.loadAddon(fit)
  term.open(container)
  // WebGL draws faster; without it xterm falls back to its DOM renderer.
  try {
    const gl = new WebglAddon()
    gl.onContextLoss(() => gl.dispose())
    term.loadAddon(gl)
  } catch {
    // No WebGL here: the DOM renderer draws the screen.
  }
  term.onData(onData)
  return {
    term,
    fit: () => {
      if (container.clientWidth === 0 || container.clientHeight === 0) return false
      try {
        fit.fit()
        return true
      } catch {
        return false
      }
    },
    setTheme: (next) => {
      term.options.theme = next
    },
    dispose: () => term.dispose(),
  }
}
