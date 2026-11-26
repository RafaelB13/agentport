// The panel's languages. English is the source language: its strings are the
// catalog keys, so it has no catalog of its own.
export const LANGS = ['en', 'pt-BR', 'es', 'zh-Hant'];

// pickLanguage applies the rule: the saved choice, then the browser's languages
// (zh-TW, zh-HK, zh-MO and zh-Hant are Traditional; zh-CN is English), then
// English. It is plain JavaScript so the Go tests can run it in Node as is.
export function pickLanguage(saved, languages) {
  if (LANGS.includes(saved)) return saved;
  for (const tag of languages && languages.length ? languages : ['en']) {
    if (/^zh-(TW|HK|MO|Hant)/i.test(tag)) return 'zh-Hant';
    if (/^zh/i.test(tag)) return 'en';
    if (/^pt/i.test(tag)) return 'pt-BR';
    if (/^es/i.test(tag)) return 'es';
    if (/^en/i.test(tag)) return 'en';
  }
  return 'en';
}
