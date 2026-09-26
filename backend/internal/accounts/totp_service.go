package accounts

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"kupol/internal/audit"
)

// Сценарии кода из приложения (TOTP). По желанию: включается в личном деле, вход без него по-прежнему работает.
//
//	подключить  → TOTPBegin (пароль; выдаёт секрет и ссылку для QR) → TOTPEnable (первый код из приложения; выдаёт
//	              одноразовые коды на случай потери телефона; остальные сессии завершаются)
//	войти       → LoginWithCode: пароль, затем код из приложения ИЛИ один из одноразовых кодов
//	выключить   → TOTPDisable (пароль и код)          новые одноразовые коды → TOTPRenewRecoveryCodes (пароль и код)
//	потерян телефон и коды → администратор: `kupol user reset-totp <логин>` (AdminResetTOTP)
//
// Неверные коды считаются теми же счётчиками, что и неверные пароли, — код из шести цифр нельзя подобрать перебором.

// TOTPSetup — что показать при подключении: секрет для ручного ввода и ссылка otpauth:// (её же кодирует QR).
type TOTPSetup struct {
	Secret string
	URI    string
}

func (s *Service) userByID(ctx context.Context, id int64) (*User, error) {
	var u User
	err := s.db.WithContext(ctx).Take(&u, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNoSession
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// TOTPBegin начинает подключение: подтверждает пароль, заводит новый секрет (прежний неподтверждённый заменяется) и отдаёт его.
// Включено всё ещё ничего: защита включается только первым верным кодом (TOTPEnable), так что ошибка при сканировании QR
// не запирает человека за собственным паролем.
func (s *Service) TOTPBegin(ctx context.Context, userID int64, password string, ci ClientInfo) (*TOTPSetup, error) {
	user, err := s.userByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user.TOTPEnabled() {
		return nil, ErrTOTPAlreadyEnabled
	}
	if err := s.verifyOwnPassword(ctx, user, password, ci); err != nil {
		return nil, err
	}
	secret, err := newTOTPSecret()
	if err != nil {
		return nil, err
	}
	sealed, err := s.box.seal(user.ID, secret)
	if err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Model(&User{}).Where("id = ? AND totp_enabled_at IS NULL", user.ID).Update("totp_pending", sealed).Error; err != nil {
		return nil, err
	}
	return &TOTPSetup{Secret: b32.EncodeToString(secret), URI: totpURI(secret, user.Login)}, nil
}

// TOTPEnable подтверждает подключение первым кодом из приложения и включает защиту. Возвращает одноразовые коды —
// они показываются один раз. Остальные сессии пользователя завершаются: включив защиту, человек не должен оставлять
// открытой сессию, которой у него может уже не быть.
func (s *Service) TOTPEnable(ctx context.Context, userID, sessionID int64, code string) ([]string, error) {
	user, err := s.userByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user.TOTPEnabled() {
		return nil, ErrTOTPAlreadyEnabled
	}
	if len(user.TOTPPending) == 0 {
		return nil, ErrTOTPNotEnabled
	}
	secret, err := s.box.open(user.ID, user.TOTPPending)
	if err != nil {
		return nil, err
	}
	step, ok := verifyTOTP(secret, code, s.now(), 0)
	if !ok {
		return nil, ErrTOTPInvalid
	}
	codes, err := newRecoveryCodes()
	if err != nil {
		return nil, err
	}
	now := s.now()
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&User{}).Where("id = ? AND totp_enabled_at IS NULL AND totp_pending = ?", user.ID, user.TOTPPending).Updates(map[string]any{
			"totp_secret": user.TOTPPending, "totp_pending": nil, "totp_enabled_at": now, "totp_last_step": step,
		})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 { // параллельное подключение успело раньше
			return ErrTOTPAlreadyEnabled
		}
		if err := s.replaceRecoveryCodes(tx, user.ID, codes, now); err != nil {
			return err
		}
		if err := audit.Record(tx, now, audit.TOTPEnabled, audit.Event{ActorID: &user.ID, TargetUserID: &user.ID}); err != nil {
			return err
		}
		return s.revokeSessions(tx, user.ID, sessionID)
	})
	if err != nil {
		return nil, err
	}
	s.log.Info("включён код из приложения", "user_id", user.ID)
	return codes, nil
}

func (s *Service) replaceRecoveryCodes(tx *gorm.DB, userID int64, codes []string, now time.Time) error {
	if err := tx.Exec("DELETE FROM totp_recovery_codes WHERE user_id = ?", userID).Error; err != nil {
		return err
	}
	for _, c := range codes {
		canon, _ := canonicalRecoveryCode(c)
		if err := tx.Exec("INSERT INTO totp_recovery_codes (user_id, code_hash, created_at) VALUES (?, ?, ?)", userID, recoveryHash(canon), now).Error; err != nil {
			return err
		}
	}
	return nil
}

// verifyOwnSecondFactor подтверждает пароль и второй фактор вошедшего пользователя (выключение, новые коды).
func (s *Service) verifyOwnSecondFactor(ctx context.Context, user *User, password, code string, ci ClientInfo) error {
	if !user.TOTPEnabled() {
		return ErrTOTPNotEnabled
	}
	if err := s.verifyOwnPassword(ctx, user, password, ci); err != nil {
		return err
	}
	if _, ok, err := s.verifySecondFactor(ctx, user, code); err != nil {
		return err
	} else if !ok {
		s.recordLoginFailure(ci.IP, user.Login)
		s.log.Warn("неверный код из приложения", "user_id", user.ID, "ip", ci.IP)
		return ErrTOTPInvalid
	}
	return nil
}

// TOTPDisable выключает код из приложения; нужны пароль и код (или одноразовый код). Одноразовые коды удаляются.
func (s *Service) TOTPDisable(ctx context.Context, userID int64, password, code string, ci ClientInfo) error {
	user, err := s.userByID(ctx, userID)
	if err != nil {
		return err
	}
	if err := s.verifyOwnSecondFactor(ctx, user, password, code, ci); err != nil {
		return err
	}
	now := s.now()
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := clearTOTP(tx, user.ID); err != nil {
			return err
		}
		return audit.Record(tx, now, audit.TOTPDisabled, audit.Event{ActorID: &user.ID, TargetUserID: &user.ID})
	})
	if err == nil {
		s.log.Info("код из приложения выключен", "user_id", user.ID)
	}
	return err
}

// TOTPRenewRecoveryCodes выдаёт новые одноразовые коды; прежние перестают действовать.
func (s *Service) TOTPRenewRecoveryCodes(ctx context.Context, userID int64, password, code string, ci ClientInfo) ([]string, error) {
	user, err := s.userByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if err := s.verifyOwnSecondFactor(ctx, user, password, code, ci); err != nil {
		return nil, err
	}
	codes, err := newRecoveryCodes()
	if err != nil {
		return nil, err
	}
	now := s.now()
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.replaceRecoveryCodes(tx, user.ID, codes, now); err != nil {
			return err
		}
		return audit.Record(tx, now, audit.TOTPCodesRenewed, audit.Event{ActorID: &user.ID, TargetUserID: &user.ID})
	})
	if err != nil {
		return nil, err
	}
	return codes, nil
}

// RecoveryCodesLeft — сколько одноразовых кодов ещё не потрачено (для личного дела).
func (s *Service) RecoveryCodesLeft(ctx context.Context, userID int64) (int, error) {
	var n int64
	err := s.db.WithContext(ctx).Raw("SELECT count(*) FROM totp_recovery_codes WHERE user_id = ? AND used_at IS NULL", userID).Scan(&n).Error
	return int(n), err
}

func clearTOTP(tx *gorm.DB, userID int64) error {
	if err := tx.Model(&User{}).Where("id = ?", userID).Updates(map[string]any{
		"totp_secret": nil, "totp_pending": nil, "totp_enabled_at": nil, "totp_last_step": 0,
	}).Error; err != nil {
		return err
	}
	return tx.Exec("DELETE FROM totp_recovery_codes WHERE user_id = ?", userID).Error
}

// AdminResetTOTP снимает код из приложения по просьбе владельца, потерявшего телефон и одноразовые коды (команда на сервере).
func (s *Service) AdminResetTOTP(ctx context.Context, login string) error {
	user, err := s.findByLogin(ctx, login)
	if err != nil {
		return err
	}
	if !user.TOTPEnabled() && len(user.TOTPPending) == 0 {
		return ErrTOTPNotEnabled
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := clearTOTP(tx, user.ID); err != nil {
			return err
		}
		return audit.Record(tx, s.now(), audit.TOTPReset, audit.Event{TargetUserID: &user.ID})
	})
}

// verifySecondFactor принимает код из приложения или одноразовый код. Тот и другой действуют один раз: шаг времени, по которому
// принят код, запоминается (условным UPDATE — два одновременных входа с одним кодом не пройдут оба), одноразовый код гасится.
// Секрет, который не удалось расшифровать (сменён общий секрет сервера), не мешает войти по одноразовому коду.
func (s *Service) verifySecondFactor(ctx context.Context, user *User, code string) (kind string, ok bool, err error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return "", false, nil
	}
	db := s.db.WithContext(ctx)
	if digits, isDigits := canonicalTOTPCode(code); isDigits {
		secret, oerr := s.box.open(user.ID, user.TOTPSecret)
		if oerr != nil {
			s.log.Error("секрет кода из приложения не расшифровывается", "user_id", user.ID, "err", oerr)
			return "", false, nil
		}
		step, good := verifyTOTP(secret, digits, s.now(), user.TOTPLastStep)
		if !good {
			return "", false, nil
		}
		res := db.Model(&User{}).Where("id = ? AND totp_last_step < ?", user.ID, step).Update("totp_last_step", step)
		if res.Error != nil {
			return "", false, res.Error
		}
		if res.RowsAffected != 1 {
			return "", false, nil
		}
		user.TOTPLastStep = step
		return "totp", true, nil
	}
	canon, good := canonicalRecoveryCode(code)
	if !good {
		return "", false, nil
	}
	now := s.now()
	err = db.Transaction(func(tx *gorm.DB) error {
		res := tx.Exec("UPDATE totp_recovery_codes SET used_at = ? WHERE user_id = ? AND code_hash = ? AND used_at IS NULL", now, user.ID, recoveryHash(canon))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return nil
		}
		ok = true
		return audit.Record(tx, now, audit.TOTPRecoveryUsed, audit.Event{ActorID: &user.ID, TargetUserID: &user.ID})
	})
	if err != nil || !ok {
		return "", false, err
	}
	return "recovery", true, nil
}
