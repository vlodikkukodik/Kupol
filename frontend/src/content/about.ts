// Текст страницы «О КУПОЛЕ» лежит в каталогах языков (src/i18n/messages/<язык>/about.ts). Здесь — только форма страницы:
// сколько абзацев и пунктов в каждом разделе. Один источник и для страницы (AboutContent.vue), и для пререндера
// (scripts/prerender: поисковик видит текст без JavaScript).
import { t } from '@/i18n'

export interface AboutSection {
  id: string
  title: string
  paragraphs: string[]
  /** Список пунктов после абзацев */
  items?: string[]
}

/** Разделы справки: сколько абзацев (p1…) и пунктов (i1…) в каждом; сами тексты — about.sections.<id>.* */
const SECTIONS = [
  { id: 'about', paragraphs: 3, items: 0 },
  { id: 'structure', paragraphs: 2, items: 5 },
  { id: 'terms', paragraphs: 1, items: 4 },
  { id: 'rules', paragraphs: 3, items: 0 },
] as const

const range = (n: number): number[] => Array.from({ length: n }, (_, i) => i + 1)

export function aboutIntro(): string {
  return t('about.intro')
}

export function aboutSections(): AboutSection[] {
  return SECTIONS.map((s) => ({
    id: s.id,
    title: t(`about.sections.${s.id}.title`),
    paragraphs: range(s.paragraphs).map((i) => t(`about.sections.${s.id}.p${i}`)),
    ...(s.items ? { items: range(s.items).map((i) => t(`about.sections.${s.id}.i${i}`)) } : {}),
  }))
}

/** Уровни допуска: звание и как его получить. Звания — те же, что показывает сервер (documents.LevelName). */
export function levelRows(): { level: number; name: string; how: string }[] {
  return range(8).map((n) => ({ level: n - 1, name: t(`levels.name.${n - 1}`), how: t(`about.levels.how.${n - 1}`) }))
}

export interface PrivacyItem {
  term: string
  text: string
}

export function privacyItems(): PrivacyItem[] {
  return range(6).map((i) => ({ term: t(`about.privacy.i${i}.term`), text: t(`about.privacy.i${i}.text`) }))
}
