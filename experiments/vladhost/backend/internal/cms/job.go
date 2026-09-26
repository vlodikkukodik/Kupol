package cms

import (
	"strconv"
	"time"

	"vladhost/internal/apperr"
)

// Шаги установки по порядку: интерфейс показывает их списком.
const (
	StepRuntime  = "runtime"  // включить PHP для сайта
	StepDatabase = "database" // создать базу
	StepDownload = "download" // скачать и проверить дистрибутив
	StepFiles    = "files"    // разложить файлы
	StepConfig   = "config"   // записать wp-config.php
	StepInstall  = "install"  // выполнить установку WordPress
	StepVerify   = "verify"   // проверить, что сайт открывается
)

// Steps — шаги в порядке выполнения.
var Steps = []string{StepRuntime, StepDatabase, StepDownload, StepFiles, StepConfig, StepInstall, StepVerify}

// Состояния установки.
const (
	JobRunning = "running"
	JobDone    = "done"
	JobFailed  = "failed"
)

// Result — итог успешной установки. Пароли видны один раз: при первом чтении итога они стираются из памяти панели.
type Result struct {
	URL           string `json:"url"`
	AdminURL      string `json:"admin_url"`
	AdminUser     string `json:"admin_user"`
	AdminPassword string `json:"admin_password"`
	DBName        string `json:"db_name"`
	DBPassword    string `json:"db_password"`
}

// Job — одна установка. Хранится в памяти: после перезапуска панели незавершённая установка теряется, а следы её отката делает сама.
type Job struct {
	SiteID  int64
	UserID  int64
	Status  string
	Step    string
	Err     *apperr.Error // причина отказа
	Detail  string        // подробности отказа (текст ошибки сервера, только для показа как есть)
	Started time.Time
	Ended   time.Time
	Result  *Result
}

// JobView — что видит интерфейс.
type JobView struct {
	Status string   `json:"status"`
	Step   string   `json:"step"`
	Steps  []string `json:"steps"`
	// Failure — код и подстановки ошибки; сообщение на языке пользователя подбирает HTTP-слой.
	Failure *Failure `json:"failure,omitempty"`
	Result  *Result  `json:"result,omitempty"`
}

// Failure — отказ на шаге.
type Failure struct {
	Code   string `json:"code"`
	Field  string `json:"field,omitempty"`
	Step   string `json:"step"`
	Detail string `json:"detail,omitempty"`
	Args   []any  `json:"-"`
}

func (s *Service) jobView(siteID int64) *JobView {
	s.mu.Lock()
	defer s.mu.Unlock()
	return viewOf(s.jobs[siteID])
}

func viewOf(j *Job) *JobView {
	if j == nil {
		return nil
	}
	v := &JobView{Status: j.Status, Step: j.Step, Steps: Steps}
	if j.Err != nil {
		v.Failure = &Failure{Code: j.Err.Code, Field: j.Err.Field, Step: j.Step, Detail: j.Detail, Args: j.Err.Args}
	}
	return v
}

// TakeJob возвращает состояние последней установки сайта. Итог с паролями отдаётся один раз.
func (s *Service) TakeJob(userID, siteID int64) (*JobView, *apperr.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j := s.jobs[siteID]
	if j == nil || j.UserID != userID {
		return nil, nil
	}
	v := viewOf(j)
	if j.Status == JobDone && j.Result != nil {
		v.Result = j.Result
		j.Result = nil // пароли больше нигде не хранятся
	}
	return v, j.Err
}

func (s *Service) setStep(j *Job, step string) {
	s.mu.Lock()
	j.Step = step
	s.mu.Unlock()
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
