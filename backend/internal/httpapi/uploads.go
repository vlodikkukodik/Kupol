package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"kupol/internal/accounts"
	"kupol/internal/uploads"
)

// Загрузки (этап 6.1). Билет выдаётся через обычный API (с сессией и CSRF), сам файл принимает /api/uploads/put/:билет —
// на боевом сервере это адрес api-поддомена: файл идёт мимо PHP-прокси, а право загружать даёт билет.

// uploadBodyLimit — предел тела запроса загрузки: файл плюс поля формы.
const uploadBodyLimit = uploads.MaxBytes + 1<<20

// uploadPutPrefix — путь загрузки: для него действует свой предел тела (см. bodyLimitMiddleware).
const uploadPutPrefix = "/api/uploads/put/"

type uploadHandlers struct {
	svc *uploads.Service
	log *slog.Logger
}

func (h *uploadHandlers) fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, uploads.ErrNotFound):
		Fail(c, http.StatusNotFound, CodeNotFound, "Файл не найден")
	case errors.Is(err, uploads.ErrBadTicket):
		Fail(c, http.StatusForbidden, CodeForbidden, "Билет на загрузку недействителен: получите новый")
	case errors.Is(err, uploads.ErrTooLarge):
		Fail(c, http.StatusRequestEntityTooLarge, CodeTooLarge, "Файл больше 20 МБ")
	case errors.Is(err, uploads.ErrUnsupported):
		FailFields(c, http.StatusUnprocessableEntity, CodeValidation, "Проверьте поля формы", map[string]string{"file": "Нужна картинка JPEG, PNG или WebP либо аудио mp3 или ogg"})
	case errors.Is(err, uploads.ErrInUse):
		Fail(c, http.StatusConflict, CodeInvalidState, "Файл используется в документе: сначала уберите его оттуда")
	default:
		h.log.Error("ошибка обработчика загрузок", "err", err, "path", c.Request.URL.Path, "request_id", RequestID(c))
		Fail(c, http.StatusInternalServerError, CodeInternal, "Сбой архива")
	}
}

func uploadActor(c *gin.Context) uploads.Actor {
	u := CurrentAuth(c).User
	return uploads.Actor{ID: u.ID, Directorate: u.Directorate, Manager: u.Can(accounts.CapEditPublished)}
}

// POST /api/team/uploads/ticket — билет на загрузку (право писать).
func (h *uploadHandlers) ticket(c *gin.Context) {
	t, exp, err := h.svc.NewTicket(CurrentAuth(c).User.ID)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, UploadTicketResponse{Ticket: t, ExpiresAt: exp.UTC(), Path: uploadPutPrefix + t, MaxBytes: uploads.MaxBytes})
}

// POST /api/uploads/put/:ticket — multipart: file и необязательный level (0–7). Авторизует билет.
func (h *uploadHandlers) put(c *gin.Context) {
	uid, err := h.svc.UseTicket(c.Param("ticket"))
	if err != nil {
		h.fail(c, err)
		return
	}
	fh, err := c.FormFile("file")
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			h.fail(c, uploads.ErrTooLarge)
			return
		}
		FailFields(c, http.StatusUnprocessableEntity, CodeValidation, "Проверьте поля формы", map[string]string{"file": "Выберите файл"})
		return
	}
	level, _ := strconv.Atoi(c.PostForm("level"))
	f, err := fh.Open()
	if err != nil {
		h.fail(c, err)
		return
	}
	defer f.Close()
	up, err := h.svc.Save(c.Request.Context(), uid, fh.Filename, level, f)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, UploadResponse{Upload: *up})
}

// GET /api/team/uploads — загрузки (свои; Директорату и Редактору — все).
func (h *uploadHandlers) list(c *gin.Context) {
	items, err := h.svc.List(c.Request.Context(), uploadActor(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, UploadsResponse{Items: items})
}

// DELETE /api/team/uploads/:id
func (h *uploadHandlers) remove(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), uploadActor(c), id); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *uploadHandlers) serve(thumb bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		v := viewerFrom(c)
		path, mime, err := h.svc.Open(c.Request.Context(), v.Level(), c.Param("key"), thumb)
		if err != nil {
			h.fail(c, err)
			return
		}
		c.Header("Content-Type", mime)
		c.Header("Cache-Control", "private, max-age=3600")
		c.File(path)
	}
}
