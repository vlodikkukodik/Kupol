package webgw

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"vladhost/internal/sitelog"
)

// statusWriter запоминает статус и число отданных байт для журнала доступа.
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 && code >= 200 { // 1xx-ответы не считаются итоговым статусом
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

// Unwrap нужен http.ResponseController (сброс буфера, дедлайны).
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// errorCode — машинный код причины для журнала ошибок; интерфейс панели переводит его на язык пользователя.
func errorCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "bad_request"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusMethodNotAllowed:
		return "method_not_allowed"
	case http.StatusGone:
		return "gone"
	case http.StatusTooManyRequests:
		return "too_many_requests"
	}
	if status >= 500 {
		return "server_error"
	}
	return "error"
}

func (h *Handler) logAccess(site string, r *http.Request, host string, w *statusWriter, start time.Time) {
	if h.logs == nil {
		return
	}
	status := w.status
	if status == 0 {
		status = http.StatusOK // обработчик ничего не писал: net/http отдаст 200
	}
	h.logs.Write(site, sitelog.KindAccess, sitelog.Entry{
		T: start.UTC(), IP: clientIP(r), Host: host, Method: r.Method, Path: r.URL.RequestURI(),
		Status: status, Bytes: w.bytes, Ms: time.Since(start).Milliseconds(),
		Referer: r.Referer(), UA: r.UserAgent(),
	})
}

func (h *Handler) logError(site string, r *http.Request, host, code, detail string) {
	if h.logs == nil {
		return
	}
	h.logs.Write(site, sitelog.KindError, sitelog.Entry{
		T: time.Now().UTC(), IP: clientIP(r), Host: host, Path: r.URL.RequestURI(), Code: code, Detail: detail,
	})
}

// MaintainLogs периодически удаляет журналы сайтов, которых больше нет, пока не отменён ctx.
func (h *Handler) MaintainLogs(ctx context.Context, every time.Duration) {
	if h.logs == nil {
		return
	}
	sweep := func() {
		if _, err := os.Stat(h.opts.Root); err != nil {
			return // каталог сайтов недоступен (не смонтирован): по этому признаку ничего не удаляем
		}
		h.logs.Sweep(func(site string) bool {
			_, err := os.Stat(filepath.Join(h.opts.Root, site))
			return err == nil
		})
	}
	sweep()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			sweep()
		}
	}
}
