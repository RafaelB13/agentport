// Vite plugins that fit the panel to the Companion that serves it:
// - the build lands in internal/companion/ui, which the Go binary embeds;
// - the page's asset URLs start with {{BASE}}, which the Companion fills with
//   /view/<token> when it serves the page;
// - the bundle names no address outside this Companion (the CSP test in
//   internal/companion/panel_test.go checks the served files the same way);
// - the dev server fills the placeholders the Companion would fill.
import fs from 'node:fs'
import path from 'node:path'
import type { Plugin } from 'vite'

// The Go test's pattern for an address outside the Companion.
const EXTERNAL_URL = /https?:\/\/[^\s"'<>)]+/g

// The project's own pages are the only addresses the panel may name.
const ALLOWED = ['https://github.com/rafaelb13/agentport']

// XML namespace names are identifiers that React and the icon library pass to
// createElementNS; a browser never loads them. They stay in the code, written
// with an escaped slash (the same string at run time) so that the served files
// carry no address at all.
const NAMESPACES = new Set([
  'http://www.w3.org/2000/svg',
  'http://www.w3.org/1999/xhtml',
  'http://www.w3.org/1999/xlink',
  'http://www.w3.org/XML/1998/namespace',
  'http://www.w3.org/1998/Math/MathML',
  'http://www.w3.org/2000/xmlns/',
])

// Links inside third-party error messages that only a developer reads. They
// keep their text without the scheme, so they read the same and are no address.
const STRIPPED = ['https://reactjs.org/docs/error-decoder.html']

// Packages whose code reaches the bundle without passing through the module
// graph: Tailwind writes its preflight (from modern-normalize) into app.css.
const EXTRA_PACKAGES = ['tailwindcss']

const LICENSES_FILE = 'assets/third-party-licenses.txt'

// packageRoot returns the directory of the npm package a module belongs to.
function packageRoot(id: string): string | null {
  const clean = id.replace(/^\0/, '').split('?')[0]
  const marker = '/node_modules/'
  const at = clean.lastIndexOf(marker)
  if (at < 0) return null
  const rest = clean.slice(at + marker.length).split('/')
  const name = rest[0].startsWith('@') ? rest.slice(0, 2).join('/') : rest[0]
  return clean.slice(0, at + marker.length) + name
}

// licenseNotices lists every bundled package with its license text: the
// minified bundle drops license comments, and this file carries them instead.
function licenseNotices(roots: Set<string>): string {
  const entries: string[] = []
  for (const root of [...roots].sort()) {
    const pkg = JSON.parse(fs.readFileSync(path.join(root, 'package.json'), 'utf8')) as {
      name: string
      version: string
      license?: string
    }
    const file = fs.readdirSync(root).find((name) => /^licen[cs]e(\.(md|txt))?$/i.test(name))
    const text = file ? fs.readFileSync(path.join(root, file), 'utf8').trim() : '(no license file in the package)'
    entries.push(`${pkg.name}@${pkg.version} (${pkg.license ?? 'see below'})\n\n${text}`)
  }
  return (
    'Third-party software in the agentport panel bundle (assets/*.js, assets/*.css).\n\n' +
    entries.join('\n\n' + '-'.repeat(78) + '\n\n') +
    '\n'
  )
}

export function companionBuild(outDir: string, nodeModules: string): Plugin {
  return {
    name: 'agentport-companion-build',
    apply: 'build',
    enforce: 'post',
    // The output directory also holds files the build does not own (the
    // catalogs and the brand marks), so only the files it writes are removed.
    buildStart() {
      fs.rmSync(path.join(outDir, 'index.html'), { force: true })
      const assets = path.join(outDir, 'assets')
      if (!fs.existsSync(assets)) return
      for (const name of fs.readdirSync(assets)) {
        if (/\.(js|css|map|txt|woff2)$/.test(name)) fs.rmSync(path.join(assets, name), { force: true })
      }
    },
    generateBundle(_options, bundle) {
      const roots = new Set(EXTRA_PACKAGES.map((name) => path.join(nodeModules, name)))
      for (const file of Object.values(bundle)) {
        if (file.type !== 'chunk') continue
        for (const id of file.moduleIds) {
          const root = packageRoot(id)
          if (root) roots.add(root)
        }
      }
      this.emitFile({ type: 'asset', fileName: LICENSES_FILE, source: licenseNotices(roots) })

      // Only markup, script and style can make the page load something.
      const problems: string[] = []
      for (const file of Object.values(bundle)) {
        if (!/\.(html|js|css)$/.test(file.fileName)) continue
        const isChunk = file.type === 'chunk'
        let text = isChunk ? file.code : typeof file.source === 'string' ? file.source : null
        if (text === null) continue
        if (file.fileName === 'index.html') {
          // Relative asset URLs break when the page is served without a trailing
          // slash; the Companion's base makes them absolute.
          text = text.replace(/(src|href)="\.\/assets\//g, '$1="{{BASE}}/assets/')
        }
        text = text.replace(EXTERNAL_URL, (url) => {
          if (ALLOWED.some((allowed) => url.startsWith(allowed))) return url
          if (STRIPPED.some((link) => url.startsWith(link))) return url.replace(/^https?:\/\//, '')
          if (isChunk && NAMESPACES.has(url)) return url.replace('://', ':\\/\\/')
          problems.push(`${file.fileName}: ${url}`)
          return url
        })
        if (isChunk) file.code = text
        else file.source = text
      }
      if (problems.length) {
        this.error(
          'The panel bundle names addresses outside this Companion; the panel CSP and its tests forbid them:\n' +
            problems.join('\n'),
        )
      }
    },
  }
}

// companionDev fills the page placeholders for the Vite dev server, which proxies
// /api and /assets to a running Companion (see web/README.md).
export function companionDev(catalogDir: string): Plugin {
  return {
    name: 'agentport-companion-dev',
    apply: 'serve',
    transformIndexHtml(html) {
      const catalogs: Record<string, unknown> = {}
      for (const name of fs.readdirSync(catalogDir)) {
        if (name.endsWith('.json')) {
          catalogs[name.slice(0, -'.json'.length)] = JSON.parse(fs.readFileSync(path.join(catalogDir, name), 'utf8'))
        }
      }
      const json = JSON.stringify(catalogs).replace(/</g, '\\u003c').replace(/>/g, '\\u003e').replace(/&/g, '\\u0026')
      return html.replaceAll('{{BASE}}', '').replaceAll('{{LANG}}', 'en').replace('{{I18N}}', json)
    },
  }
}
