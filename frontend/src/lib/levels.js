// Уровни допуска (спецификация §3) для подписей в интерфейсе. Сервер — источник истины:
// названия совпадают с documents.LevelName в backend/internal/documents/viewer.go.

export const LEVEL_NAMES = [
  'Гражданин',
  'Посетитель',
  'Стажёр',
  'Сотрудник',
  'Надзиратель',
  'Куратор',
  'Особый Совет',
  'Директорат',
]

export const MAX_LEVEL = LEVEL_NAMES.length - 1

export function levelName(level) {
  return LEVEL_NAMES[level] ?? ''
}

/** «допуск не ниже уровня 4 (Надзиратель)»; для уровня 7 — «только Директорат». */
export function requiredAccess(level) {
  if (level >= MAX_LEVEL) return 'только Директорат'
  return `допуск не ниже уровня ${level} (${levelName(level)})`
}
