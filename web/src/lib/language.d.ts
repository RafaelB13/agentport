export type Lang = 'en' | 'pt-BR' | 'es' | 'zh-Hant'

export declare const LANGS: readonly Lang[]

export declare function pickLanguage(
  saved: string | null | undefined,
  languages: readonly string[] | null | undefined,
): Lang
