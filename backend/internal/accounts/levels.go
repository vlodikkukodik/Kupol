// Package accounts — пользователи, серверные сессии, вход, регистрация, восстановление доступа.
package accounts

import "kupol/internal/i18n"

// Уровни допуска (спецификация §3). Гражданин — посетитель без входа, в БД его нет.
const (
	LevelGuest    = 0
	LevelVisitor  = 1 // регистрация
	LevelIntern   = 2 // Стажёр: XP (~неделя)
	LevelEmployee = 3 // Сотрудник: XP (~месяц)
	LevelWarden   = 4 // Надзиратель: выдаёт Особый Совет
	LevelCurator  = 5 // Куратор: выдаёт Особый Совет
	LevelCouncil  = 6 // Особый Совет: выдаёт Особый Совет
)

var levelNames = [...]string{
	LevelGuest:    "Гражданин",
	LevelVisitor:  "Посетитель",
	LevelIntern:   "Стажёр",
	LevelEmployee: "Сотрудник",
	LevelWarden:   "Надзиратель",
	LevelCurator:  "Куратор",
	LevelCouncil:  "Особый Совет",
}

// DirectorateName — звание Директората: он стоит вне лестницы уровней и выдаётся только автором.
const DirectorateName = "Директорат"

// LevelName возвращает звание по-русски. Директорат показывается вместо уровня.
func LevelName(level int, directorate bool) string { return LevelNameIn(i18n.RU, level, directorate) }

// LevelNameIn — звание на языке l.
func LevelNameIn(l i18n.Lang, level int, directorate bool) string {
	if directorate {
		return l.Translate(DirectorateName)
	}
	if level < 0 || level >= len(levelNames) {
		return ""
	}
	return l.Translate(levelNames[level])
}

// ValidLevel — допустимый уровень зарегистрированного пользователя.
func ValidLevel(level int) bool { return level >= LevelVisitor && level <= LevelCouncil }
