package scanner

import (
	"io/fs"
	"path/filepath"
	"strings"
)

var supportedExtensions = map[string]bool{
	".epub": true,
	".pdf":  true,
	".mobi": true,
	".azw3": true,
}

// Scan recursively walks root and returns the path of every file whose
// extension is one of the v1 supported ebook formats.
func Scan(root string) ([]string, error) {
	var results []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if supportedExtensions[ext] {
			results = append(results, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}
