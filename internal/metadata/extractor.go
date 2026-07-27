package metadata

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/FrancisChung/book-organiser/internal/textutil"
)

// CoverExtractorVersion identifies the current cover-selection/extraction
// logic across all formats (see extractPDF, extractEpub, extractMobi and
// everything they call into for cover bytes). internal/librarian.Scan
// stores this alongside each cached book so a cached CoverPath can be
// distinguished from one produced by an older, since-improved version of
// this logic -- a mismatch is treated as a cache miss even when the
// source file's ModTime/Size haven't changed, forcing exactly one
// re-extraction that then re-caches under the current version. Bump this
// whenever a change here could cause an already-scanned file to yield
// different cover bytes than before (e.g. the page-aware PDF cover walk
// added in Plan A, or a new colorspace/filter becoming decodable) -- not
// for changes that only affect Title/Author/Year, and not for changes
// that only affect files this logic couldn't already handle.
const CoverExtractorVersion = 8 // bumped: composite-cover detection now recurses into a page's Form XObjects to find a text-show operator nested inside one (previously only the page's own /Contents stream was checked directly), so a page that wraps its whole visual content in a single top-level Form no longer returns just its lone embedded image instead of the full composited page.

// MetadataExtractorVersion identifies the current Title/Author/Year
// extraction logic, parallel to CoverExtractorVersion but tracked
// separately since a change to one rarely affects the other -- bumping
// CoverExtractorVersion for a Title/Author-only change would force every
// book's already-correct cover to be needlessly re-extracted and
// re-cached, and vice versa. internal/librarian.Scan stores this
// alongside each cached book the same way CoverVersion is stored,
// forcing exactly one re-extraction whenever it's stale relative to a
// cached entry. Bump this whenever a change here could cause an
// already-scanned file to yield a different Title/Author/Year than
// before -- not for changes that only affect cover bytes.
const MetadataExtractorVersion = 5 // bumped: unescapePDFBytes now decodes \ddd octal-escape sequences in literal strings (previously only \n/\r/\t were recognized; any other escaped character, including an octal digit, passed through as literal text) -- a Title/Author using this escape shape now decodes correctly instead of showing garbled digit text.

// Extract dispatches to the appropriate format-specific extractor based on
// path's extension, then cleans the Title/Author it returns -- embedded
// metadata not infrequently carries a stray trailing "." or ";" (leftover
// sentence punctuation, or a dangling author-list separator), multiple
// authors are sometimes ";"-separated rather than the app's ","-separated
// convention, and titles sometimes use "_"/"-" as word separators or
// inconsistent casing. hyphenExceptions lists hyphenated words FormatTitle
// should keep hyphenated rather than splitting on "-"
// (cfg.TitleFormatting.HyphenExceptions). It is the only function other
// packages should call for whole-book extraction. ListPDFCoverCandidates
// and ExtractPDFPageCover (pdf_override.go) are the two exceptions: both
// exist specifically for the manual cover-override picker, which needs
// page-level granularity this combined Result can't expose. Safe to call
// concurrently for different files: this package's decoders operate only
// on their own local data, and the one piece of process-wide shared state
// (the PDFium renderer, pdf_render.go) serializes itself internally via
// pdfiumMu -- callers never need their own synchronization.
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
