// Package pdf — печать документа в PDF (этап 6.2): блоки читателя → разметка Typst → typst compile.
//
// Лист собирается по тому, что видит читатель: блоки уже отфильтрованы по допуску (documents.RenderBlocks),
// закрытые фрагменты становятся чёрными полосами, закрытые блоки — плашками с нужным уровнем. Шрифты Typst
// (IBM Plex Mono, Oswald) вшиты в бинарь, поэтому сборка одинакова на любой машине.
//
// Сборка идёт вне HTTP-запроса — в очереди (см. Queue, этап 6.2 по architecture.md): typst занимает
// от долей секунды до пары секунд, запросу такое не место.
package pdf

import (
	"context"
	"strings"

	"kupol/internal/documents"
	"kupol/internal/i18n"
)

// Options — что нужно, чтобы собрать лист читателя.
type Options struct {
	// Lang — язык подписей (экз., «Данные удалены», названия полей).
	Lang i18n.Lang
	// SiteOrigin — https://… для ссылок на другие документы; пусто — ссылки без адреса не собираются.
	SiteOrigin string
	// ViewerLevel — допуск читателя: по нему открываются файлы изображений.
	ViewerLevel int
	// OpenImage — путь к файлу изображения по ключу загрузки; nil — изображения не подставляются.
	OpenImage func(key string) (string, error)
}

// Render — PDF документа глазами читателя. Требует typst (см. docs/deploy.md).
func Render(ctx context.Context, doc *documents.OutDocument, opts Options) ([]byte, error) {
	r, err := DefaultRunner()
	if err != nil {
		return nil, err
	}
	return r.Render(ctx, doc, opts)
}

// Render — то же своим компилятором (его держит очередь).
func (r *Runner) Render(ctx context.Context, doc *documents.OutDocument, opts Options) ([]byte, error) {
	if opts.Lang != i18n.IT {
		opts.Lang = i18n.RU
	}
	s := &sheet{
		doc:    doc,
		lang:   opts.Lang,
		origin: strings.TrimRight(opts.SiteOrigin, "/"),
		level:  opts.ViewerLevel,
		images: map[string]string{},
		open:   opts.OpenImage,
	}

	body, err := s.blocks()
	if err != nil {
		return nil, err
	}
	author := ""
	if doc.Author != nil {
		author = *doc.Author
	}

	var src strings.Builder
	src.WriteString(s.setup(doc.Title, author))
	src.WriteString("\n\n")
	src.WriteString(s.titleBlock())
	src.WriteString("\n\n")
	src.WriteString(body)
	src.WriteString("\n")
	src.WriteString(s.mentionsBlock())
	src.WriteString("\n")
	src.WriteString(s.authorFooter())
	src.WriteString("\n")

	return r.Compile(ctx, []byte(src.String()), s.images)
}
