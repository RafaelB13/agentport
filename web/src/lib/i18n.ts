import { createContext, useContext } from 'react'
import { type Lang } from './language.js'

// Strings are written in English in the code and looked up in the catalogs the
// Companion embeds in the page (internal/companion/ui/assets/i18n). The Go
// tests read every literal key of the source and check each catalog has it.

type Catalog = Record<string, string>

const CATALOGS: Partial<Record<Lang, Catalog>> = (() => {
  try {
    const text = document.getElementById('agentport-i18n')?.textContent ?? ''
    return (JSON.parse(text) as Partial<Record<Lang, Catalog>>) || {}
  } catch {
    return {}
  }
})()

export const LANG_NAMES: Record<Lang, string> = {
  en: 'English',
  'pt-BR': 'Português (Brasil)',
  es: 'Español',
  'zh-Hant': '繁體中文',
}

export const LANG_SHORT: Record<Lang, string> = { en: 'EN', 'pt-BR': 'PT', es: 'ES', 'zh-Hant': '中文' }

export type Vars = Record<string, string | number>
export type Translate = (key: string, vars?: Vars) => string

// translate returns the text of an English key in a language, with {name}
// placeholders filled in. An untranslated key shows its English text.
export function translate(lang: Lang, key: string, vars?: Vars): string {
  let text = (lang !== 'en' && CATALOGS[lang]?.[key]) || key
  if (vars) {
    for (const [name, value] of Object.entries(vars)) text = text.split('{' + name + '}').join(String(value))
  }
  return text
}

export interface I18n {
  lang: Lang
  setLang: (lang: Lang) => void
  t: Translate
}

export const I18nContext = createContext<I18n | null>(null)

export function useI18n(): I18n {
  const value = useContext(I18nContext)
  if (!value) throw new Error('useI18n outside I18nProvider')
  return value
}
