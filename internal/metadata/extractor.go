package metadata

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Extract dispatches to the appropriate format-specific extractor based on
// path's extension. It is the only function other packages should call.
func Extract(path string) (Result, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".epub":
		return extractEpub(path)
	case ".pdf":
		return extractPDF(path)
	case ".mobi", ".azw3":
		return extractMobi(path)
	default:
		return Result{}, fmt.Errorf("unsupported extension: %s", ext)
	}
}
