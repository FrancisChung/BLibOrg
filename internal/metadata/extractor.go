package metadata

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/FrancisChung/book-organiser/internal/textutil"
)

// Extract dispatches to the appropriate format-specific extractor based on
// path's extension, then cleans the Title/Author it returns -- embedded
// metadata not infrequently carries a stray trailing "." or ";" (leftover
// sentence punctuation, or a dangling author-list separator), multiple
// authors are sometimes ";"-separated rather than the app's ","-separated
// convention, and titles sometimes use "_"/"-" as word separators or
// inconsistent casing. hyphenExceptions lists hyphenated words FormatTitle
// should keep hyphenated rather than splitting on "-"
// (cfg.TitleFormatting.HyphenExceptions). It is the only function other
// packages should call.
func Extract(path string, hyphenExceptions []string, pdfCoverPageLimit int) (Result, error) {
	ext := strings.ToLower(filepath.Ext(path))
	var result Result
	var err error
	switch ext {
	case ".epub":
		result, err = extractEpub(path)
	case ".pdf":
		result, err = extractPDF(path, pdfCoverPageLimit)
	case ".mobi", ".azw3":
		result, err = extractMobi(path)
	default:
		return Result{}, fmt.Errorf("unsupported extension: %s", ext)
	}
	if err != nil {
		return Result{}, err
	}
	result.Title = textutil.CleanField(result.Title)
	result.Title = textutil.FormatTitle(result.Title, hyphenExceptions)
	result.Author = textutil.CleanField(result.Author)
	result.Author = textutil.NormalizeAuthorSeparators(result.Author)
	return result, nil
}
