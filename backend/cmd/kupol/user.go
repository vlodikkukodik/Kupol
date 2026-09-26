package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"kupol/internal/accounts"
)

const userUsage = `использование:
  kupol user show <логин>                     показать пользователя
  kupol user set-level <логин> <1-6>          задать уровень допуска
  kupol user set-directorate <логин> on|off   выдать или снять Директорат
  kupol user set-role <логин> <роль> on|off   выдать или снять роль команды: author, editor, moderator, archivist
  kupol user reset-password <логин>           сбросить пароль (выводит временный, завершает все сессии)
  kupol user reset-totp <логин>               снять код из приложения (человек потерял телефон и одноразовые коды)`

// runUser — административные команды над пользователями. Директорат выдаётся только здесь
// (автор, на сервере), уровни 4–6 до появления админки (этап 7) — тоже.
func runUser(ctx context.Context, svc *accounts.Service, args []string, out io.Writer) error {
	if len(args) < 2 {
		return errors.New(userUsage)
	}
	cmd, login := args[0], args[1]
	rest := args[2:]

	switch cmd {
	case "show":
		if len(rest) != 0 {
			return errors.New(userUsage)
		}
		u, err := svc.Find(ctx, login)
		if err != nil {
			return userErr(login, err)
		}
		m, err := svc.MemberByLogin(ctx, login)
		if err != nil {
			return userErr(login, err)
		}
		roles := "—"
		if len(m.Roles) > 0 {
			names := make([]string, len(m.Roles))
			for i, r := range m.Roles {
				names[i] = fmt.Sprintf("%s (%s)", r, r.Name())
			}
			roles = strings.Join(names, ", ")
		}
		last := "—"
		if u.LastLoginAt != nil {
			last = u.LastLoginAt.UTC().Format("2006-01-02 15:04:05 UTC")
		}
		fmt.Fprintf(out, "логин:        %s\nуровень:      %d (%s)\nДиректорат:   %v\nроли:         %s\nзарегистрирован: %s\nпоследний вход:  %s\n",
			u.Login, u.Level, accounts.LevelName(u.Level, false), u.Directorate, roles, u.CreatedAt.UTC().Format("2006-01-02 15:04:05 UTC"), last)
		return nil

	case "set-role":
		if len(rest) != 2 {
			return errors.New(userUsage)
		}
		role, ok := accounts.ParseRole(rest[0])
		if !ok {
			return fmt.Errorf("неизвестная роль %q; допустимы: author, editor, moderator, archivist", rest[0])
		}
		var on bool
		switch strings.ToLower(rest[1]) {
		case "on":
			on = true
		case "off":
			on = false
		default:
			return errors.New("ожидается on или off")
		}
		var changed bool
		var err error
		verb := "снята"
		if on {
			verb = "выдана"
			_, changed, err = svc.GrantRole(ctx, login, role, nil)
		} else {
			_, changed, err = svc.RevokeRole(ctx, login, role, nil)
		}
		if err != nil {
			return userErr(login, err)
		}
		if !changed {
			fmt.Fprintf(out, "%s: роль %s (%s) уже была в этом состоянии, ничего не изменилось\n", login, role, role.Name())
			return nil
		}
		fmt.Fprintf(out, "%s: роль %s (%s) %s\n", login, role, role.Name(), verb)
		return nil

	case "set-level":
		if len(rest) != 1 {
			return errors.New(userUsage)
		}
		level, err := strconv.Atoi(rest[0])
		if err != nil {
			return fmt.Errorf("уровень должен быть числом от %d до %d", accounts.LevelVisitor, accounts.LevelCouncil)
		}
		if err := svc.SetLevel(ctx, login, level); err != nil {
			return userErr(login, err)
		}
		fmt.Fprintf(out, "%s: уровень %d (%s)\n", login, level, accounts.LevelName(level, false))
		return nil

	case "set-directorate":
		if len(rest) != 1 {
			return errors.New(userUsage)
		}
		var on bool
		switch strings.ToLower(rest[0]) {
		case "on":
			on = true
		case "off":
			on = false
		default:
			return errors.New("ожидается on или off")
		}
		if err := svc.SetDirectorate(ctx, login, on); err != nil {
			return userErr(login, err)
		}
		state := "снят"
		if on {
			state = "выдан"
		}
		fmt.Fprintf(out, "%s: Директорат %s\n", login, state)
		return nil

	case "reset-password":
		if len(rest) != 0 {
			return errors.New(userUsage)
		}
		temp, err := svc.AdminResetPassword(ctx, login)
		if err != nil {
			return userErr(login, err)
		}
		fmt.Fprintf(out, "%s: пароль сброшен, все сессии завершены.\nВременный пароль (показывается один раз): %s\nПопросите пользователя сменить его в личном деле.\n", login, temp)
		return nil

	case "reset-totp":
		if len(rest) != 0 {
			return errors.New(userUsage)
		}
		if err := svc.AdminResetTOTP(ctx, login); err != nil {
			if errors.Is(err, accounts.ErrTOTPNotEnabled) {
				return fmt.Errorf("у %q код из приложения не включён", login)
			}
			return userErr(login, err)
		}
		fmt.Fprintf(out, "%s: код из приложения и одноразовые коды сняты. Вход — по паролю; включить защиту можно снова в личном деле.\n", login)
		return nil
	}
	return fmt.Errorf("неизвестная команда user %q\n%s", cmd, userUsage)
}

func userErr(login string, err error) error {
	if errors.Is(err, accounts.ErrUserNotFound) {
		return fmt.Errorf("пользователь %q не найден", login)
	}
	return err
}
