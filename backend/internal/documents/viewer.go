package documents

// Уровни допуска документов и блоков (спецификация §3).
const (
	LevelPublic      = 0 // Гражданин: без регистрации
	LevelCouncil     = 6 // Особый Совет
	LevelDirectorate = 7 // только Директорат
	MaxLevel         = LevelDirectorate
)

// Viewer — читатель: кто и с каким допуском открывает документ.
type Viewer struct {
	UserID      int64 // 0 — Гражданин (без входа)
	UserLevel   int   // уровень зарегистрированного пользователя, 1–6
	Directorate bool
}

// Guest — Гражданин.
var Guest = Viewer{}

// Level — эффективный уровень допуска: у Директората выше всех (7), у гостя 0.
func (v Viewer) Level() int {
	switch {
	case v.Directorate:
		return LevelDirectorate
	case v.UserID == 0:
		return LevelPublic
	case v.UserLevel < 1:
		return 1
	case v.UserLevel > LevelCouncil:
		return LevelCouncil
	default:
		return v.UserLevel
	}
}

// SeesUnpublished — видит ли читатель черновики, документы на проверке и в архиве.
// Пока это только Директорат; роли персонала (этап 3) расширят правило.
func (v Viewer) SeesUnpublished() bool { return v.Directorate }

// LevelName — название уровня для интерфейса («уровень 3», «только Директорат»).
func LevelName(level int) string {
	switch level {
	case LevelPublic:
		return "Гражданин"
	case 1:
		return "Посетитель"
	case 2:
		return "Стажёр"
	case 3:
		return "Сотрудник"
	case 4:
		return "Надзиратель"
	case 5:
		return "Куратор"
	case LevelCouncil:
		return "Особый Совет"
	case LevelDirectorate:
		return "Директорат"
	}
	return ""
}
