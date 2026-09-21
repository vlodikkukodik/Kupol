// Уровни допуска (спецификация §3) для подписей в интерфейсе. Названия — в каталоге языка (levels.name.N);
// сервер отдаёт те же названия на языке запроса (documents.LevelName в backend/internal/documents/viewer.go).
import { t } from '@/i18n'

export const MAX_LEVEL = 7

export function levelName(level: number): string {
  return level >= 0 && level <= MAX_LEVEL ? t(`levels.name.${level}`) : ''
}

/** Названия всех уровней 0–7 на текущем языке (для списков выбора; вызывать внутри computed, чтобы список менялся вместе с языком). */
export function levelNames(): string[] {
  return Array.from({ length: MAX_LEVEL + 1 }, (_, i) => levelName(i))
}

/** «допуск не ниже уровня 4 (Надзиратель)»; для уровня 7 — «только Директорат». */
export function requiredAccess(level: number): string {
  if (level >= MAX_LEVEL) return t('levels.directorateOnly')
  return t('levels.atLeast', { level, name: levelName(level) })
}
