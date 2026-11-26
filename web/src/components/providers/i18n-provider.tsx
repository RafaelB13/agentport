import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { setFailureText } from '@/lib/api'
import { I18nContext, translate, type Translate } from '@/lib/i18n'
import { LANGS, pickLanguage, type Lang } from '@/lib/language.js'
import { readStored, writeStored } from '@/lib/storage'

function browserLanguages(): readonly string[] {
  if (navigator.languages && navigator.languages.length) return navigator.languages
  return [navigator.language || 'en']
}

export function I18nProvider({ children }: { children: ReactNode }) {
  const [lang, setLangState] = useState<Lang>(() => pickLanguage(readStored('agentport.lang'), browserLanguages()))
  const t = useCallback<Translate>((key, vars) => translate(lang, key, vars), [lang])

  useEffect(() => {
    document.documentElement.lang = lang
    writeStored('agentport.lang', lang)
    setFailureText((status) => t('Request failed ({status})', { status }))
  }, [lang, t])

  const setLang = useCallback((next: Lang) => setLangState(LANGS.includes(next) ? next : 'en'), [])
  const value = useMemo(() => ({ lang, setLang, t }), [lang, setLang, t])
  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>
}
