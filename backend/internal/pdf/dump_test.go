package pdf

import (
	"os"
	"testing"

	"kupol/internal/documents"

	"kupol/internal/i18n"
)

// TestDumpSources — вспомогательный: пишет разметку в /tmp/kupol-pdf-dump для разбора ошибок typst.
func TestDumpSources(t *testing.T) {
	if os.Getenv("KUPOL_DUMP") == "" {
		t.Skip("включается KUPOL_DUMP=1")
	}
	os.MkdirAll("/tmp/kupol-pdf-dump", 0o755)
	for _, tc := range []string{"every", "adv"} {
		s, doc := dumpSheet(t, tc)
		body, err := s.blocks()
		if err != nil {
			t.Fatal(err)
		}
		author := ""
		if doc.Author != nil {
			author = *doc.Author
		}
		src := s.setup(doc.Title, author) + "\n\n" + s.titleBlock() + "\n\n" + body + "\n" + s.mentionsBlock() + "\n" + s.authorFooter() + "\n"
		os.WriteFile("/tmp/kupol-pdf-dump/"+tc+".typ", []byte(src), 0o644)
	}
	_ = i18n.RU
}

func dumpSheet(t *testing.T, which string) (*sheet, *documents.OutDocument) {
	t.Helper()
	if which == "adv" {
		doc := adversarialDoc()
		return &sheet{doc: doc, lang: i18n.RU, origin: "https://kupol.test", level: 7, images: map[string]string{}}, doc
	}
	return newSheet(t)
}
