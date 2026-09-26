// Грамоты (достижения, шаг 5.6): фиксированный набор из шести. Названия — в каталоге языка
// (achievements.name.<kind>); сервер отдаёт те же названия на языке запроса (achievements.Kind.NameIn
// в backend/internal/achievements/achievements.go) — здесь дублируются для мгновенного показа тоста
// сразу после входа/погашения кода, без похода на сервер за списком.
import { t } from '@/i18n'

export const ACHIEVEMENT_KINDS = ['read_10', 'read_50', 'read_100', 'streak_7', 'streak_30', 'secret_finder'] as const

export function achievementName(kind: string): string {
  return (ACHIEVEMENT_KINDS as readonly string[]).includes(kind) ? t(`achievements.name.${kind}`) : kind
}
