package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"vladhost/internal/activity"
)

// WithActivity подключает журнал действий аккаунта.
func WithActivity(svc *activity.Service) Option {
	return func(s *Server) { s.activity = svc }
}

// auditTargetKey: обработчик может назвать объект действия (сайт, домен, база) — он пишется в журнал вместе с событием.
const auditTargetKey = "audit_target"

func setAuditTarget(c *gin.Context, target string) { c.Set(auditTargetKey, target) }

// audit записывает в журнал успешные изменяющие запросы вошедшего пользователя (что означает маршрут — таблица activity.RouteKinds). Имя сайта
// определяется до обработчика: у удалённого сайта его после уже не найти.
func (s *Server) audit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if s.activity == nil {
			c.Next()
			return
		}
		method, route := c.Request.Method, c.FullPath()
		key := method + " " + route
		kind, named := activity.KindOf(key)
		mutating := method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete
		if (!mutating && !activity.Audited(key)) || (named && kind == activity.Skip) {
			c.Next()
			return
		}
		if !named {
			kind = "other"
		}
		target := ""
		if strings.HasPrefix(route, "/api/sites/:id") && s.sites != nil {
			if id, err := strconv.ParseInt(c.Param("id"), 10, 64); err == nil {
				if site, err := s.sites.Get(c.Request.Context(), c.GetInt64("uid"), id); err == nil {
					target = site.Host
				}
			}
		}
		c.Next()
		if c.Writer.Status() >= http.StatusBadRequest {
			return
		}
		if v, ok := c.Get(auditTargetKey); ok {
			if t, ok := v.(string); ok && t != "" {
				target = t
			}
		}
		if kind == "other" && target == "" {
			target = key
		}
		s.record(c, c.GetInt64("uid"), kind, target)
	}
}

// record пишет событие с адресом и программой клиента.
func (s *Server) record(c *gin.Context, userID int64, kind, target string) {
	s.activity.Record(c.Request.Context(), activity.Input{UserID: userID, Kind: kind, Target: target, IP: c.ClientIP(), UserAgent: c.Request.UserAgent()})
}

// listActivity отдаёт журнал вошедшего пользователя: свежие сверху, страницами по курсору.
func (s *Server) listActivity(c *gin.Context) {
	if s.activity == nil {
		fail(c, http.StatusNotFound, "not_found")
		return
	}
	category := c.Query("category")
	if category != "" {
		known := category == activity.CatOther
		for _, k := range activity.Categories() {
			known = known || k == category
		}
		if !known {
			fail(c, http.StatusBadRequest, "bad_request")
			return
		}
	}
	before, _ := strconv.ParseInt(c.Query("before"), 10, 64)
	limit, _ := strconv.Atoi(c.Query("limit"))
	events, next, err := s.activity.List(c.Request.Context(), c.GetInt64("uid"), category, before, limit)
	if err != nil {
		failErr(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"events": events, "next": next, "categories": activity.Categories()})
}
