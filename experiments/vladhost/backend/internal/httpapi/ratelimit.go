package httpapi

import (
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

type visitor struct {
	lim  *rate.Limiter
	seen time.Time
}

// ipLimiter — ограничитель запросов по IP в памяти процесса (панель работает одним процессом).
type ipLimiter struct {
	mu        sync.Mutex
	visitors  map[string]*visitor
	rate      rate.Limit
	burst     int
	lastPrune time.Time
}

func newIPLimiter(perMinute, burst int) *ipLimiter {
	return &ipLimiter{
		visitors: map[string]*visitor{},
		rate:     rate.Limit(float64(perMinute) / 60),
		burst:    burst,
	}
}

func (l *ipLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if now.Sub(l.lastPrune) > 5*time.Minute {
		for k, v := range l.visitors {
			if now.Sub(v.seen) > 10*time.Minute {
				delete(l.visitors, k)
			}
		}
		l.lastPrune = now
	}
	v, ok := l.visitors[ip]
	if !ok {
		v = &visitor{lim: rate.NewLimiter(l.rate, l.burst)}
		l.visitors[ip] = v
	}
	v.seen = now
	return v.lim.Allow()
}

func (l *ipLimiter) middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !l.allow(c.ClientIP()) {
			c.Header("Retry-After", "60")
			fail(c, 429, "too_many_requests", "Слишком много попыток, подождите минуту")
			return
		}
		c.Next()
	}
}
