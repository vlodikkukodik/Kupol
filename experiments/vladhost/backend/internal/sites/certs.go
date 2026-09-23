package sites

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Сертификаты выпускает отдельная root-служба (deploy/vladhost-certs.sh): панель работает без прав root.
// Обмен через каталог CertsDir:
//
//	queue/issue-{host}, queue/delete-{host} — заявки (пишет панель, каталог её);
//	status/{host}                          — результат: "ok" или "error: …" (пишет root, каталог root'а).
//
// Каталог status принадлежит root, чтобы панель (а значит и любой, кто её взломает) не могла подсунуть
// симлинк и заставить root-скрипт перезаписать чужой файл.

const certPendingTimeout = 30 * time.Minute

func (s *Service) certsEnabled() bool { return s.certsDir != "" }

func (s *Service) enqueue(kind, host string) error {
	dir := filepath.Join(s.certsDir, "queue")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Содержимое не используется — важно только имя, поэтому временных файлов нет
	// (иначе systemd-триггер сработал бы на недописанный файл).
	return os.WriteFile(filepath.Join(dir, kind+"-"+host), nil, 0o644)
}

// RetryCert повторяет выпуск сертификата для сайта, где он не удался.
func (s *Service) RetryCert(ctx context.Context, userID, id int64) (*Site, error) {
	site, err := s.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if !s.certsEnabled() || site.CertStatus != CertFailed {
		return nil, ErrCertState
	}
	if err := s.enqueue("issue", site.Host); err != nil {
		return nil, err
	}
	now := time.Now()
	site.CertStatus, site.CertError = CertPending, ""
	err = s.db.WithContext(ctx).Model(site).Updates(map[string]any{
		"cert_status": CertPending, "cert_error": "", "cert_requested_at": now,
	}).Error
	return site, err
}

// WatchCerts раз в interval сверяет заявки в состоянии pending с результатом выпускателя.
func (s *Service) WatchCerts(ctx context.Context, interval time.Duration) {
	if !s.certsEnabled() {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if err := s.reconcileCerts(ctx); err != nil && ctx.Err() == nil {
			log.Printf("сертификаты: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Service) reconcileCerts(ctx context.Context) error {
	var pending []Site
	if err := s.db.WithContext(ctx).Where("cert_status = ?", CertPending).Find(&pending).Error; err != nil {
		return err
	}
	for _, site := range pending {
		status, msg, ok := s.readCertStatus(site)
		if !ok {
			if site.CertRequestedAt != nil && time.Since(*site.CertRequestedAt) > certPendingTimeout {
				status, msg = CertFailed, "выпускатель сертификатов не ответил"
			} else {
				continue
			}
		}
		if err := s.db.WithContext(ctx).Model(&Site{}).Where("id = ?", site.ID).
			Updates(map[string]any{"cert_status": status, "cert_error": msg}).Error; err != nil {
			return err
		}
	}
	return nil
}

// readCertStatus учитывает только результат, полученный после последней заявки:
// файл, оставшийся от прошлой попытки, не должен закрыть новую.
func (s *Service) readCertStatus(site Site) (status, msg string, ok bool) {
	path := filepath.Join(s.certsDir, "status", site.Host)
	fi, err := os.Stat(path)
	if err != nil || (site.CertRequestedAt != nil && !fi.ModTime().After(*site.CertRequestedAt)) {
		return "", "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", false
	}
	text := strings.TrimSpace(string(data))
	if text == "ok" {
		return CertActive, "", true
	}
	if rest, found := strings.CutPrefix(text, "error:"); found {
		return CertFailed, strings.TrimSpace(rest), true
	}
	return "", "", false
}

var ErrCertState = errors.New("для этого сайта нельзя повторить выпуск сертификата")
