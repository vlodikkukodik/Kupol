package httpapi

import (
	"errors"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"

	"vladhost/internal/sites"
)

func (s *Server) listFiles(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	entries, err := s.sites.ListDir(c.Request.Context(), c.GetInt64("uid"), id, c.Query("path"))
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"entries": entries})
}

func (s *Server) readFile(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	data, err := s.sites.ReadFile(c.Request.Context(), c.GetInt64("uid"), id, c.Query("path"))
	if err != nil {
		failErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"content": string(data), "size": len(data)})
}

func (s *Server) saveFile(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	// JSON с экранированием может быть чуть больше самого текста.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 3*sites.MaxEditBytes)
	var in struct {
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		if _, tooBig := errors.AsType[*http.MaxBytesError](err); tooBig {
			failErr(c, sites.ErrTooLarge)
			return
		}
		fail(c, http.StatusBadRequest, "bad_request", "Некорректный запрос")
		return
	}
	err := s.sites.WriteFile(c.Request.Context(), c.GetInt64("uid"), id, c.Query("path"),
		strings.NewReader(in.Content), sites.MaxEditBytes)
	if err != nil {
		failErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) deleteFile(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	if err := s.sites.Remove(c.Request.Context(), c.GetInt64("uid"), id, c.Query("path")); err != nil {
		failErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) mkdir(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	var in struct {
		Path string `json:"path"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, http.StatusBadRequest, "bad_request", "Некорректный запрос")
		return
	}
	if err := s.sites.Mkdir(c.Request.Context(), c.GetInt64("uid"), id, in.Path); err != nil {
		failErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) renameFile(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	var in struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, http.StatusBadRequest, "bad_request", "Некорректный запрос")
		return
	}
	if err := s.sites.Rename(c.Request.Context(), c.GetInt64("uid"), id, in.From, in.To); err != nil {
		failErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// uploadFile принимает файл в папку ?path= под его собственным именем.
func (s *Server) uploadFile(c *gin.Context) {
	id, ok := siteID(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, s.sites.Limits().DiskQuotaBytes+(1<<20))
	fh, err := c.FormFile("file")
	if err != nil {
		if _, tooBig := errors.AsType[*http.MaxBytesError](err); tooBig {
			failErr(c, sites.ErrQuota)
			return
		}
		fail(c, http.StatusBadRequest, "bad_request", "Приложите файл в поле file")
		return
	}
	name := path.Base(strings.ReplaceAll(fh.Filename, "\\", "/"))
	if name == "." || name == ".." || name == "/" {
		failErr(c, sites.ErrBadPath)
		return
	}
	f, err := fh.Open()
	if err != nil {
		failErr(c, err)
		return
	}
	defer func() { _ = f.Close() }()
	target := path.Join(c.Query("path"), name)
	if err := s.sites.WriteFile(c.Request.Context(), c.GetInt64("uid"), id, target, f, 0); err != nil {
		failErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
