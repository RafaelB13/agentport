# Painel do Companion (`web/`)

O painel que o Companion serve em `http://127.0.0.1:<porta>/view/<token>/` é
este app: Vite + React 18 + TypeScript + Tailwind CSS + [shadcn/ui] (estilo
new-york, cor zinc), com ícones lucide-react e o terminal em xterm.js.

[shadcn/ui]: https://ui.shadcn.com

## Build

```bash
cd web
npm ci
npm run build   # tsc -b && vite build
npm run lint
```

O build escreve em `internal/companion/ui/` (`index.html` e `assets/app.js`,
`assets/app.css`, o chunk do terminal e `assets/third-party-licenses.txt`),
que o binário Go embute com `go:embed`. **O resultado é versionado**: assim
`go build`/`go install` funcionam sem Node. Depois de mudar `web/`, rode
`npm run build` e faça commit também de `internal/companion/ui/`; o CI refaz o
build e falha se houver diferença.

Ficam em `internal/companion/ui/assets/` e não são gerados pelo build:

- `i18n/*.json`: catálogos pt-BR, es e zh-Hant. O inglês é a chave: no código,
  sempre `t('Texto em inglês')` com a chave literal entre aspas simples. Uma
  chave nova entra nos três catálogos (os testes Go conferem);
- `agentport-mark.svg` e `favicon.svg`, iguais aos de
  `design-systems/agentport-companion/assets/`.

O Companion preenche `{{BASE}}` (`/view/<token>`), `{{LANG}}` e `{{I18N}}` no
`index.html`. O app lê a base de `<meta name="agentport-base">` e a usa para a
API e os assets; o plugin em `build/companion.ts` escreve `{{BASE}}/assets/...`
nas URLs da página e barra qualquer endereço externo no bundle (a CSP e os
testes de `internal/companion` exigem isso).

## Desenvolvimento com o Vite

1. Inicie um Companion (`agentport companion start <perfil>`) e abra o painel
   (`agentport companion open <perfil>`). Copie a URL da barra de endereço.
2. Rode o dev server apontando para ela; `/api` (inclusive o WebSocket do
   terminal) e `/assets` vão por proxy para o Companion:

```bash
cd web
AGENTPORT_PANEL_URL='http://127.0.0.1:<porta>/view/<token>/' npm run dev
```

3. Abra `http://localhost:5173/`. Sem `AGENTPORT_PANEL_URL` o app sobe, mas a
   API não responde.

## Componentes

Os componentes de `src/components/ui/` vêm do CLI do shadcn
(`npx shadcn@2.10.0 add <componente>`, configurado em `components.json`) e
podem ser editados como código do projeto. As dependências têm versão exata
(`.npmrc` com `save-exact`) e o `package-lock.json` é versionado.
