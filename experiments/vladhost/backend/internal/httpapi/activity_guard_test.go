package httpapi_test

import (
	"os"
	"sort"
	"strings"
	"testing"

	"vladhost/internal/activity"
)

// Каждый маршрут, который меняет данные вошедшего пользователя, обязан быть в таблице журнала (или явно помечен «не писать»): добавили новую
// возможность — тест напомнит решить, как её называть в журнале. И наоборот: в таблице нет опечаток, указывающих на несуществующие маршруты.
func TestEveryMutatingRouteHasAnAuditDecision(t *testing.T) {
	e := newEnv(t)
	e.withActivity()
	routes := map[string]bool{}
	var missing []string
	for _, r := range e.r.Routes() {
		key := r.Method + " " + r.Path
		routes[key] = true
		mutating := r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH" || r.Method == "DELETE"
		// вход, регистрация и подобное пишутся явно; внутренний обмен токенами между панелью и веб-клиентом не действие пользователя
		if !mutating || strings.HasPrefix(r.Path, "/api/auth/") || strings.HasPrefix(r.Path, "/api/internal/") {
			continue
		}
		if _, ok := activity.KindOf(key); !ok {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("маршруты без решения для журнала (добавьте в activity.RouteKinds): %v", missing)
	}
	var stray []string
	for key := range activity.RouteKinds {
		if !routes[key] {
			stray = append(stray, key)
		}
	}
	sort.Strings(stray)
	if len(stray) > 0 {
		t.Fatalf("в таблице журнала есть несуществующие маршруты: %v", stray)
	}
}

// У каждого события журнала есть подпись в интерфейсе (иначе пользователь увидел бы «Другое действие»), и каждое имеет известный вид.
func TestEveryAuditKindHasAFrontendLabel(t *testing.T) {
	raw, err := os.ReadFile("../../../frontend/src/views/ActivityView.vue")
	if err != nil {
		t.Skipf("нет исходников интерфейса: %v", err)
	}
	src := string(raw)
	kinds := map[string]bool{
		activity.KindLogin: true, activity.KindLoginFailed: true, activity.KindLogout: true, activity.KindRegister: true, activity.KindPasswordReset: true,
		activity.KindEmailVerified: true, activity.KindFTPLogin: true,
	}
	for _, k := range activity.RouteKinds {
		if k != activity.Skip {
			kinds[k] = true
		}
	}
	var missing, uncategorised []string
	for k := range kinds {
		if !strings.Contains(src, "'"+k+"':") {
			missing = append(missing, k)
		}
		if activity.CategoryOf(k) == activity.CatOther {
			uncategorised = append(uncategorised, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(uncategorised)
	if len(missing) > 0 {
		t.Errorf("нет подписи в ActivityView.vue: %v", missing)
	}
	if len(uncategorised) > 0 {
		t.Errorf("события без вида (activity.CategoryOf): %v", uncategorised)
	}
}
