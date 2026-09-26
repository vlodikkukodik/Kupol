package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"kupol/internal/database"
	"kupol/internal/version"
)

type HealthResponse struct {
	Status   string `json:"status"`
	DB       string `json:"db"`
	Version  string `json:"version"`
	ClientIP string `json:"client_ip"`
	IPSource string `json:"ip_source"`
}

// healthHandler — /health (напрямую, для мониторинга) и /api/health (через PHP-прокси).
// Отдаёт 503, если БД недоступна.
func healthHandler(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()

		resp := HealthResponse{
			Status:   "ok",
			DB:       "ok",
			Version:  version.Version,
			ClientIP: ClientIP(c),
			IPSource: IPSource(c),
		}
		code := http.StatusOK
		if err := database.Ping(ctx, db); err != nil {
			resp.Status, resp.DB = "fail", "unavailable"
			code = http.StatusServiceUnavailable
		}
		c.JSON(code, resp)
	}
}
