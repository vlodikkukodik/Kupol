package sites

import (
	"context"
	"errors"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"vladhost/internal/apperr"
	"vladhost/internal/sitecfg"
)

// MaxFTPAccounts — сколько дополнительных FTP-аккаунтов можно завести на сайт.
const MaxFTPAccounts = 5

var (
	ErrFTPNameInvalid   = apperr.Validation("name", "ftp_name", "invalid FTP account name")
	ErrFTPNameTaken     = apperr.New(http.StatusConflict, "ftp_name_taken", "FTP account name already taken").OnField("name")
	ErrFTPAccountLimit  = apperr.New(http.StatusForbidden, "ftp_account_limit", "FTP account limit reached").With(MaxFTPAccounts)
	ErrFTPAccountAbsent = apperr.New(http.StatusNotFound, "ftp_account_not_found", "FTP account not found")
	ErrFTPDirInvalid    = apperr.Validation("dir", "ftp_dir", "invalid FTP folder")
	ErrFTPReadOnly      = apperr.New(http.StatusForbidden, "ftp_read_only", "this FTP account is read-only")
)

// Имя аккаунта — первая часть логина: {имя}.{сайт}.{пользователь}. Точки в имени нельзя (они разделяют части логина).
var ftpAccountNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,22}[a-z0-9])?$`)

// FTPAccount — дополнительный FTP-доступ к сайту.
type FTPAccount struct {
	ID           int64      `gorm:"primaryKey" json:"id"`
	SiteID       int64      `json:"-"`
	Name         string     `json:"name"`
	PasswordHash string     `json:"-"`
	Dir          string     `json:"dir"` // подпапка сайта, которой ограничен аккаунт; пусто — весь сайт
	ReadOnly     bool       `json:"read_only"`
	Enabled      bool       `json:"enabled"`
	LastLoginAt  *time.Time `json:"last_login_at"`
	CreatedAt    time.Time  `json:"created_at"`
}

// FTPAccountUsername — логин аккаунта: имя плюс логин основного доступа сайта.
func (s *Service) FTPAccountUsername(host, name string) string {
	return name + "." + s.FTPUsername(host)
}

// errBadDir — папка не прошла проверку; вызывающий код превращает её в свою ошибку (FTP-аккаунт, домен).
var errBadDir = errors.New("invalid site folder")

// cleanSiteDir приводит папку внутри сайта к виду "a/b" (пусто — весь сайт). Правила общие с настройками сайта и шлюзом
// (sitecfg.CleanDir): только вниз от корня, без скрытых частей пути, глубина ограничена.
func cleanSiteDir(dir string) (string, error) {
	c, ok := sitecfg.CleanDir(dir)
	if !ok {
		return "", errBadDir
	}
	return c, nil
}

func normalizeFTPDir(dir string) (string, error) {
	c, err := cleanSiteDir(dir)
	if err != nil {
		return "", ErrFTPDirInvalid
	}
	return c, nil
}

// ensureSiteDir создаёт папку внутри public сайта, если её нет. Через os.Root: ссылки наружу не пройдут.
func (s *Service) ensureSiteDir(site *Site, dir string) error {
	if dir == "" {
		return nil
	}
	pub := s.publicDir(site.Host)
	if err := os.MkdirAll(pub, 0o755); err != nil {
		return err
	}
	root, err := os.OpenRoot(pub)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if err := root.MkdirAll(dir, 0o755); err != nil {
		return errBadDir
	}
	fi, err := root.Lstat(dir)
	if err != nil || !fi.IsDir() {
		return errBadDir // на этом месте лежит файл или ссылка
	}
	return nil
}

// ensureFTPDir — то же для папки FTP-аккаунта.
func (s *Service) ensureFTPDir(site *Site, dir string) error {
	err := s.ensureSiteDir(site, dir)
	if errors.Is(err, errBadDir) {
		return ErrFTPDirInvalid
	}
	return err
}

func (s *Service) getFTPAccount(ctx context.Context, userID, siteID, id int64) (*FTPAccount, *Site, error) {
	site, err := s.Get(ctx, userID, siteID)
	if err != nil {
		return nil, nil, err
	}
	var a FTPAccount
	err = s.db.WithContext(ctx).Where("id = ? AND site_id = ?", id, site.ID).First(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, ErrFTPAccountAbsent
	}
	return &a, site, err
}

func mapUniqueFTPName(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return ErrFTPNameTaken
	}
	return err
}

func hashPassword(pw string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(h), err
}

// CreateFTPAccount заводит аккаунт и возвращает его пароль (он показывается один раз, хранится только bcrypt).
func (s *Service) CreateFTPAccount(ctx context.Context, userID, siteID int64, name, dir string, readOnly bool) (*FTPAccount, *Site, string, error) {
	site, err := s.Get(ctx, userID, siteID)
	if err != nil {
		return nil, nil, "", err
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if !ftpAccountNameRe.MatchString(name) {
		return nil, nil, "", ErrFTPNameInvalid
	}
	dir, err = normalizeFTPDir(dir)
	if err != nil {
		return nil, nil, "", err
	}
	var count int64
	if err := s.db.WithContext(ctx).Model(&FTPAccount{}).Where("site_id = ?", site.ID).Count(&count).Error; err != nil {
		return nil, nil, "", err
	}
	if count >= MaxFTPAccounts {
		return nil, nil, "", ErrFTPAccountLimit
	}
	if err := s.ensureFTPDir(site, dir); err != nil {
		return nil, nil, "", err
	}
	pw, err := newFTPPassword()
	if err != nil {
		return nil, nil, "", err
	}
	hash, err := hashPassword(pw)
	if err != nil {
		return nil, nil, "", err
	}
	a := &FTPAccount{SiteID: site.ID, Name: name, PasswordHash: hash, Dir: dir, ReadOnly: readOnly, Enabled: true}
	if err := s.db.WithContext(ctx).Create(a).Error; err != nil {
		return nil, nil, "", mapUniqueFTPName(err)
	}
	return a, site, pw, nil
}

// FTPAccountPatch — что менять; nil-поля остаются как были.
type FTPAccountPatch struct {
	Enabled  *bool
	ReadOnly *bool
	Dir      *string
}

// UpdateFTPAccount меняет настройки аккаунта. Открытые сессии аккаунта закрываются: новые правила действуют сразу.
func (s *Service) UpdateFTPAccount(ctx context.Context, userID, siteID, id int64, p FTPAccountPatch) (*FTPAccount, *Site, error) {
	a, site, err := s.getFTPAccount(ctx, userID, siteID, id)
	if err != nil {
		return nil, nil, err
	}
	changes := map[string]any{}
	if p.Enabled != nil {
		changes["enabled"], a.Enabled = *p.Enabled, *p.Enabled
	}
	if p.ReadOnly != nil {
		changes["read_only"], a.ReadOnly = *p.ReadOnly, *p.ReadOnly
	}
	if p.Dir != nil {
		dir, err := normalizeFTPDir(*p.Dir)
		if err != nil {
			return nil, nil, err
		}
		if err := s.ensureFTPDir(site, dir); err != nil {
			return nil, nil, err
		}
		changes["dir"], a.Dir = dir, dir
	}
	if len(changes) == 0 {
		return a, site, nil
	}
	if err := s.db.WithContext(ctx).Model(a).Updates(changes).Error; err != nil {
		return nil, nil, err
	}
	s.revokeFTPAccount(a.ID)
	return a, site, nil
}

// ResetFTPAccountPassword выдаёт аккаунту новый пароль; старый и открытые сессии перестают действовать.
func (s *Service) ResetFTPAccountPassword(ctx context.Context, userID, siteID, id int64) (*FTPAccount, *Site, string, error) {
	a, site, err := s.getFTPAccount(ctx, userID, siteID, id)
	if err != nil {
		return nil, nil, "", err
	}
	pw, err := newFTPPassword()
	if err != nil {
		return nil, nil, "", err
	}
	hash, err := hashPassword(pw)
	if err != nil {
		return nil, nil, "", err
	}
	if err := s.db.WithContext(ctx).Model(a).Update("password_hash", hash).Error; err != nil {
		return nil, nil, "", err
	}
	s.revokeFTPAccount(a.ID)
	return a, site, pw, nil
}

func (s *Service) DeleteFTPAccount(ctx context.Context, userID, siteID, id int64) error {
	a, _, err := s.getFTPAccount(ctx, userID, siteID, id)
	if err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Delete(a).Error; err != nil {
		return err
	}
	s.revokeFTPAccount(a.ID)
	return nil
}

// FTPAccountsByUser — аккаунты всех сайтов пользователя, сгруппированные по сайтам.
func (s *Service) FTPAccountsByUser(ctx context.Context, userID int64) (map[int64][]FTPAccount, error) {
	var rows []FTPAccount
	err := s.db.WithContext(ctx).Table("ftp_accounts").Select("ftp_accounts.*").
		Joins("JOIN sites ON sites.id = ftp_accounts.site_id").Where("sites.user_id = ?", userID).
		Order("ftp_accounts.id").Find(&rows).Error
	out := map[int64][]FTPAccount{}
	for _, a := range rows {
		out[a.SiteID] = append(out[a.SiteID], a)
	}
	return out, err
}

func (s *Service) revokeFTPAccount(id int64) {
	s.ftpMu.Lock()
	s.ftpAcctRevoked[id] = time.Now()
	s.ftpMu.Unlock()
}

func (s *Service) acctRevokedSince(id int64, t time.Time) bool {
	s.ftpMu.Lock()
	defer s.ftpMu.Unlock()
	r, ok := s.ftpAcctRevoked[id]
	return ok && !r.Before(t)
}

// ftpAccountLogin проверяет вход дополнительного аккаунта: name — первая часть логина, siteLogin — остальное («blog.john»).
func (s *Service) ftpAccountLogin(ctx context.Context, name, siteLogin, password string) (*FTPSession, error) {
	var site Site
	var acct FTPAccount
	err := s.db.WithContext(ctx).Where("host = ?", siteLogin+"."+s.baseDomain).First(&site).Error
	if err == nil {
		err = s.db.WithContext(ctx).Where("site_id = ? AND name = ? AND enabled = true", site.ID, name).First(&acct).Error
	}
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			_ = bcrypt.CompareHashAndPassword(dummyFTPHash, []byte(password))
			return nil, ErrFTPAuth
		}
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(acct.PasswordHash), []byte(password)) != nil {
		return nil, ErrFTPAuth
	}
	usage, err := s.acquireUsage(ctx, &site)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	s.db.WithContext(ctx).Model(&FTPAccount{}).Where("id = ?", acct.ID).Update("last_login_at", now)
	return &FTPSession{s: s, site: site, usage: usage, since: now, acctID: acct.ID, base: acct.Dir, readOnly: acct.ReadOnly}, nil
}
