import path from 'node:path'
import { defineConfig, loadEnv, type ProxyOptions } from 'vite'
import react from '@vitejs/plugin-react'
import { companionBuild, companionDev } from './build/companion'

const ui = path.resolve(__dirname, '../internal/companion/ui')

// A Companion's panel lives under /view/<token>/. For the dev server, set
// AGENTPORT_PANEL_URL to that address (agentport panel prints it); /api and
// /assets are proxied there.
function companionProxy(target: string | undefined): Record<string, ProxyOptions> | undefined {
  if (!target) return undefined
  const url = new URL(target.endsWith('/') ? target : target + '/')
  const base = url.pathname.replace(/\/$/, '')
  const options: ProxyOptions = {
    target: url.origin,
    changeOrigin: true,
    rewrite: (p) => base + p,
    ws: true,
    // The Companion accepts a terminal only from its own origin.
    configure: (proxy) => {
      proxy.on('proxyReqWs', (request) => request.setHeader('origin', url.origin))
    },
  }
  return { '/api': options, '/assets': { ...options, ws: false } }
}

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), 'AGENTPORT_')
  return {
    // Relative URLs: lazy chunks load next to the script, wherever the
    // Companion serves it; the page's own URLs get {{BASE}} at build time.
    base: './',
    plugins: [react(), companionDev(path.join(ui, 'assets/i18n')), companionBuild(ui, path.resolve(__dirname, 'node_modules'))],
    // License comments leave the minified files; the build writes every bundled
    // package's notice to assets/third-party-licenses.txt instead.
    esbuild: { legalComments: 'none' },
    resolve: {
      alias: {
        '@': path.resolve(__dirname, './src'),
      },
    },
    server: {
      port: 5173,
      strictPort: false,
      proxy: companionProxy(env.AGENTPORT_PANEL_URL),
    },
    build: {
      outDir: ui,
      // The directory also holds the catalogs and brand marks, which are not
      // build output; companionBuild removes the files of the previous build.
      emptyOutDir: false,
      assetsDir: 'assets',
      target: 'es2020',
      sourcemap: false,
      modulePreload: { polyfill: false },
      // The font files stay files: the CSP's default-src 'self' allows no
      // data: fonts.
      assetsInlineLimit: 0,
      // Stable names: the Companion sends every asset with no-store, so hashes
      // buy nothing, and the committed build diffs cleanly.
      rollupOptions: {
        output: {
          entryFileNames: 'assets/app.js',
          chunkFileNames: 'assets/[name].js',
          assetFileNames: (asset) =>
            (asset.names ?? []).some((name) => name === 'index.css') ? 'assets/app.css' : 'assets/[name][extname]',
        },
      },
    },
  }
})
