package httpapi

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"vladhost/internal/auth"
)

// checkOrigin защищает cookie-эндпоинты: запрос из браузера с чужого источника (например, со страницы
// пользовательского сайта на соседнем поддомене — для SameSite это «свой» сайт) отклоняется.
// Запросы без Origin (не браузер) проходят: им cookie всё равно взять негде.
func (s *Server) checkOrigin(c *gin.Context) {
	if o := c.GetHeader("Origin"); s.cfg.PanelOrigin != "" && o != "" && o != s.cfg.PanelOrigin {
		fail(c, http.StatusForbidden, "forbidden_origin")
		return
	}
	c.Next()
}

func (s *Server) requireAuth(c *gin.Context) {
	h := c.GetHeader("Authorization")
	token, ok := strings.CutPrefix(h, "Bearer ")
	if !ok || token == "" {
		fail(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	claims, err := s.svc.ParseAccess(token)
	if err != nil {
		fail(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	c.Set("uid", claims.UserID)
	c.Set("role", string(claims.Role))
	c.Set("sid", claims.SessionID)
	c.Next()
}

// clientMiddleware кладёт адрес и программу клиента в контекст: при входе они записываются в сессию (список «Активные сессии»).
func clientMiddleware(c *gin.Context) {
	c.Request = c.Request.WithContext(auth.WithClient(c.Request.Context(), auth.Client{IP: c.ClientIP(), UserAgent: c.Request.UserAgent()}))
	c.Next()
}

func (s *Server) requireAdmin(c *gin.Context) {
	if c.GetString("role") != string(auth.RoleAdmin) {
		fail(c, http.StatusForbidden, "forbidden")
		return
	}
	c.Next()
}
