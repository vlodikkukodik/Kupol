package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"kupol/internal/documents"
)

const docUsage = `использование:
  kupol doc import [--dry-run] [--author <логин>] <файл.json>...   загрузить документы (создаёт и обновляет)
  kupol doc export <шифр>                                          вывести документ в формате загрузки
  kupol doc list [--status draft|review|published|archived]        список всех документов
  kupol doc set-status <шифр> <статус>                             draft | review | published | archived
  kupol doc delete <шифр> --yes                                    удалить документ навсегда

Формат файла — docs/documents.md. --dry-run проверяет файл и показывает, что будет сделано, ничего не сохраняя.`

// runDoc — административные команды над документами (до появления редактора в этапе 3 это основной способ
// наполнять архив). Работает без ограничений допуска: команда запускается на сервере автором.
func runDoc(ctx context.Context, svc *documents.Service, args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New(docUsage)
	}
	cmd, rest := args[0], args[1:]

	switch cmd {
	case "import":
		return docImport(ctx, svc, rest, out)

	case "export":
		if len(rest) != 1 {
			return errors.New(docUsage)
		}
		raw, err := svc.Export(ctx, rest[0])
		if err != nil {
			return docErr(rest[0], err)
		}
		_, err = fmt.Fprintf(out, "%s\n", raw)
		return err

	case "list":
		status := ""
		switch {
		case len(rest) == 0:
		case len(rest) == 2 && rest[0] == "--status":
			status = rest[1]
		default:
			return errors.New(docUsage)
		}
		return docList(ctx, svc, status, out)

	case "set-status":
		if len(rest) != 2 {
			return errors.New(docUsage)
		}
		d, err := svc.SetStatus(ctx, rest[0], documents.Status(rest[1]))
		if err != nil {
			return docErr(rest[0], err)
		}
		fmt.Fprintf(out, "%s: статус «%s» (%s)\n", *d.Code, d.Status, documents.Status(d.Status).Name())
		return nil

	case "delete":
		if len(rest) != 2 || rest[1] != "--yes" {
			return errors.New("удаление необратимо: добавьте --yes\n" + docUsage)
		}
		if err := svc.Delete(ctx, rest[0]); err != nil {
			return docErr(rest[0], err)
		}
		fmt.Fprintf(out, "%s: удалён\n", rest[0])
		return nil
	}
	return fmt.Errorf("неизвестная команда doc %q\n%s", cmd, docUsage)
}

func docImport(ctx context.Context, svc *documents.Service, args []string, out io.Writer) error {
	var opt documents.ImportOptions
	var files []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--dry-run":
			opt.DryRun = true
		case a == "--author":
			if i+1 >= len(args) {
				return errors.New("после --author нужен логин")
			}
			i++
			opt.AuthorLogin = args[i]
		case strings.HasPrefix(a, "--author="):
			opt.AuthorLogin = strings.TrimPrefix(a, "--author=")
		case strings.HasPrefix(a, "--"):
			return fmt.Errorf("неизвестный параметр %s\n%s", a, docUsage)
		default:
			files = append(files, a)
		}
	}
	if len(files) == 0 {
		return errors.New("не указан файл\n" + docUsage)
	}

	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		rep, err := svc.Import(ctx, raw, opt)
		if err != nil {
			var ve *documents.ValidationError
			if errors.As(err, &ve) {
				fmt.Fprintf(out, "%s: документ не загружен, замечаний — %d:\n", f, len(ve.Problems))
				for _, p := range ve.Problems {
					fmt.Fprintf(out, "  %s\n", p)
				}
				return fmt.Errorf("%s: файл не прошёл проверку", f)
			}
			return fmt.Errorf("%s: %w", f, err)
		}
		if opt.DryRun {
			fmt.Fprintf(out, "%s: проверка пройдена, сохранено ничего не будет:\n", f)
		} else {
			fmt.Fprintf(out, "%s:\n", f)
		}
		for _, it := range rep.Items {
			verb := "обновлён"
			if it.Created {
				verb = "создан"
			}
			if opt.DryRun {
				verb = "будет " + map[bool]string{true: "создан", false: "обновлён"}[it.Created]
			}
			fmt.Fprintf(out, "  %-16s %-10s редакция %d, %s — %s\n", it.Code, verb, it.Revision, it.Status, it.Title)
			if it.AssignedCode {
				fmt.Fprintf(out, "    присвоен номер %s: добавьте в файл \"code\": %q, иначе повторная загрузка создаст новый документ\n", it.Code, it.Code)
			}
			for _, w := range it.Warnings {
				fmt.Fprintf(out, "    ⚠ %s\n", w)
			}
		}
	}
	return nil
}

func docList(ctx context.Context, svc *documents.Service, status string, out io.Writer) error {
	total := int64(0)
	for page := 1; ; page++ {
		res, err := svc.List(ctx, documents.DirectorateViewer, documents.ListQuery{Status: status, Page: page, PerPage: 100})
		if err != nil {
			return err
		}
		for _, it := range res.Items {
			fmt.Fprintf(out, "%-16s %-10s %-10s уровень %d  %s\n", it.Code, it.Type, it.Status, it.Level, it.Title)
		}
		total = res.Total
		if page >= res.Pages {
			break
		}
	}
	fmt.Fprintf(out, "всего: %d\n", total)
	return nil
}

func docErr(ref string, err error) error {
	if errors.Is(err, documents.ErrNotFound) {
		return fmt.Errorf("документ %q не найден", ref)
	}
	return err
}
