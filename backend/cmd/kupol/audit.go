package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"kupol/internal/audit"
)

const auditUsage = `использование:
  kupol audit [--limit N] [--action событие] [--doc <номер документа>]   показать журнал событий, новые сверху

События: role.granted, role.revoked, lock.broken, document.rolled_back, document.published_edited,
account.password_changed, account.access_restored, account.password_reset. По умолчанию — 50 последних.`

// runAudit — просмотр журнала событий на сервере (веб-страница журнала появится вместе с админкой, этап 7).
func runAudit(ctx context.Context, db *gorm.DB, args []string, out io.Writer) error {
	q := audit.Query{}
	for i := 0; i < len(args); i++ {
		if i+1 >= len(args) {
			return errors.New(auditUsage)
		}
		name, value := args[i], args[i+1]
		i++
		switch name {
		case "--limit":
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 || n > 500 {
				return errors.New("--limit: число от 1 до 500")
			}
			q.Limit = n
		case "--action":
			q.Action = audit.Action(value)
		case "--doc":
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil || n < 1 {
				return errors.New("--doc: номер документа — положительное число")
			}
			q.DocumentID = n
		default:
			return errors.New(auditUsage)
		}
	}

	rows, err := audit.List(ctx, db, q)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		_, err = fmt.Fprintln(out, "журнал пуст")
		return err
	}
	for _, r := range rows {
		who := "сервер"
		if r.Actor != nil {
			who = *r.Actor
		}
		parts := []string{r.At.Format("2006-01-02 15:04:05 UTC"), fmt.Sprintf("%-26s", r.Action), "исполнитель: " + who}
		if r.Target != nil {
			parts = append(parts, "над: "+*r.Target)
		}
		if r.DocumentID != nil {
			parts = append(parts, "документ: "+strconv.FormatInt(*r.DocumentID, 10))
		}
		if r.Details != "{}" {
			parts = append(parts, r.Details)
		}
		if _, err := fmt.Fprintln(out, strings.Join(parts, "  ")); err != nil {
			return err
		}
	}
	return nil
}
