package sites

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path"

	"vladhost/internal/webgw/htaccess"
)

const maxHtaccessFiles = 50

// HtaccessFile — результат проверки одного .htaccess сайта.
type HtaccessFile struct {
	Path  string          `json:"path"` // например ".htaccess" или "docs/.htaccess"
	Diags []htaccess.Diag `json:"diags"`
}

// CheckHtaccess разбирает все .htaccess сайта теми же правилами, что и веб-шлюз, и возвращает замечания:
// какие директивы не поддерживаются и будут проигнорированы. Ничего не меняет.
func (s *Service) CheckHtaccess(ctx context.Context, userID, id int64) ([]HtaccessFile, error) {
	out := []HtaccessFile{}
	err := s.run(ctx, userID, id, false, func(_ *Site, root *os.Root, _ int64) error {
		return fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil || len(out) >= maxHtaccessFiles {
				return nil
			}
			if d.IsDir() || d.Name() != ".htaccess" || !d.Type().IsRegular() {
				return nil
			}
			f, err := root.Open(p)
			if err != nil {
				return nil
			}
			defer func() { _ = f.Close() }()
			data, err := io.ReadAll(io.LimitReader(f, htaccess.MaxSize+1))
			if err != nil {
				return nil
			}
			diags := htaccess.Parse(data).Diags
			if diags == nil {
				diags = []htaccess.Diag{} // в JSON — пустой список, а не null
			}
			out = append(out, HtaccessFile{Path: path.Clean(p), Diags: diags})
			return nil
		})
	})
	return out, err
}
