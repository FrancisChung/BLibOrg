// Package librarian walks the already-organized library folder
// (cfg.General.LibraryFolder) and reports what's in it, grouped by the
// Category/Subcategory folder structure rename.BuildPath already produces.
// Unlike internal/pipeline, it never computes a destination or moves
// anything -- it only reads back what's already there.
//
// Title/Author/Year/CoverPath/CoverOverridden come from a persisted scan
// cache (internal/librarycache) keyed by each file's ModTime and Size,
// *and* by metadata.CoverExtractorVersion; the expensive
// metadata.Extract/covercache.GetOverride/covercache.Force trio only runs
// for a file that's new, edited, cached under an older cover-extraction
// version, or when forceRefresh is true. Because the cached fields are
// the *final, override-resolved* result, a cache hit never re-checks the
// override store -- internal/appapi's SetCoverOverride/
// SetCoverOverrideCustom/ClearCoverOverride are responsible for calling
// librarycache.Invalidate so the next Scan treats a changed book as a
// miss and re-resolves it.
package librarian

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/FrancisChung/book-organiser/internal/config"
	"github.com/FrancisChung/book-organiser/internal/covercache"
	"github.com/FrancisChung/book-organiser/internal/heuristics"
	"github.com/FrancisChung/book-organiser/internal/librarycache"
	"github.com/FrancisChung/book-organiser/internal/metadata"
	"github.com/FrancisChung/book-organiser/internal/scanner"
)

// Book is one already-organized library file, with Category/Subcategory
// read directly from its folder location rather than recomputed.
type Book struct {
	SourcePath      string
	Format          string
	Title           string
	Author          string
	Year            string
	Category        string
	Subcategory     string
	CoverPath       string // "" if no cover was found; otherwise a /covers/... URL path
	CoverOverridden bool   // true if a manual cover override (see internal/covercache) is in effect
}

// extractFunc is a seam so tests can verify metadata.Extract is skipped for
// a cache hit; production code always uses metadata.Extract.
var extractFunc = metadata.Extract

// Scan walks cfg.General.LibraryFolder for every supported ebook file,
// deriving each book's Category/Subcategory from its position in the
// <library>/<Category>/<Subcategory>/<file> layout rename.BuildPath
// produces. A file sitting directly in <library>/ (no Category folder) or
// in <library>/<Category>/ with no Subcategory folder gets an empty
// Subcategory (and, for the former, an empty Category too) rather than
// being skipped -- Scan reports what it finds, it doesn't enforce layout.
//
// Per-file Title/Author/Year/CoverPath/CoverOverridden are served from the
// persisted scan cache whenever the file's current ModTime and Size match
// what's cached AND the cached entry's CoverVersion matches the current
// metadata.CoverExtractorVersion, skipping metadata.Extract and cover
// resolution entirely. A cache miss (new file, edited file, or an entry
// cached under an older CoverVersion -- including the zero value any
// entry persisted before this field existed unmarshals to) or
// forceRefresh=true runs today's extract-then-override-check logic and
// updates the cache with the resolved result, always via
// covercache.Force rather than covercache.Ensure -- once Scan (via the
// checks above) has already decided re-extraction is warranted, the
// freshly-produced cover must actually be written, not silently skipped
// because some earlier cover already happens to sit at that path with a
// newer mtime than the book file (which is normal: covercache's own
// filename is a function of sourcePath, not of the image bytes, so an
// old, wrong cover and a new, correct one for the same book share the
// same cache path). Files no longer present on disk are dropped from the
// saved cache. A file metadata.Extract fails on (e.g. corrupt) still gets
// a Book entry with empty Title/Author/Year/CoverPath rather than being
// dropped, so it's still visible on its shelf; such a file is never
// cached, so it's retried on every subsequent Scan until it succeeds or is
// removed.
func Scan(cfg config.Config, forceRefresh bool) ([]Book, error) {
	paths, err := scanner.Scan(cfg.General.LibraryFolder)
	if err != nil {
		return nil, err
	}

	cache := librarycache.Load(cfg.General.LogFolder)
	seen := make(map[string]bool, len(paths))

	books := make([]Book, 0, len(paths))
	for _, path := range paths {
		seen[path] = true

		rel, err := filepath.Rel(cfg.General.LibraryFolder, path)
		if err != nil {
			continue
		}
		parts := strings.Split(filepath.ToSlash(filepath.Dir(rel)), "/")

		b := Book{
			SourcePath: path,
			Format:     strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
		}
		if len(parts) >= 1 && parts[0] != "." {
			b.Category = parts[0]
		}
		if len(parts) >= 2 {
			b.Subcategory = parts[1]
		}

		info, statErr := os.Stat(path)
		if statErr != nil {
			books = append(books, b)
			continue
		}

		if !forceRefresh {
			if entry, ok := cache.Fresh(path, info.ModTime(), info.Size()); ok && entry.CoverVersion == metadata.CoverExtractorVersion && entry.MetadataVersion == metadata.MetadataExtractorVersion {
				b.Title = entry.Title
				b.Author = entry.Author
				b.Year = entry.Year
				b.CoverPath = entry.CoverPath
				b.CoverOverridden = entry.CoverOverridden
				books = append(books, b)
				continue
			}
		}

		if res, err := extractFunc(path, cfg.TitleFormatting.HyphenExceptions, cfg.General.PDFCoverPageLimit); err == nil {
			b.Title = res.Title
			b.Author = res.Author
			b.Year = res.Year

			// Mirrors internal/pipeline.Run's existing filename-heuristic
			// fallback: embedded metadata sometimes resolves to nothing
			// usable (a missing field, or extractEpub/extractPDF's own
			// placeholder-value checks blanking a known-junk value), and
			// the Library view previously had no fallback at all for that
			// case, unlike Scan & Review.
			if b.Title == "" || b.Author == "" || b.Year == "" {
				stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
				h := heuristics.Parse(stem, cfg.Heuristics.KnownJunkTags)
				if b.Title == "" && h.Title != "" {
					b.Title = h.Title
				}
				if b.Author == "" && h.Author != "" {
					b.Author = h.Author
				}
				if b.Year == "" && h.Year != "" {
					b.Year = h.Year
				}
			}

			coverBytes, coverContentType := res.CoverBytes, res.CoverContentType

			// A manual override, if one is set for this book, replaces the
			// cover portion of Extract's result -- but Extract itself still
			// ran above, so Title/Author/Year are never lost for an
			// overridden book (see this plan's Global Constraints for why
			// this deliberately narrows the design doc's "extraction is
			// skipped entirely" wording to cover selection only).
			if ov, found, ovErr := covercache.GetOverride(cfg.General.LogFolder, path); ovErr == nil && found {
				b.CoverOverridden = true
				switch ov.Type {
				case covercache.OverrideCustom:
					b.CoverPath = ov.ImagePath
					coverBytes = nil // already have a stable URL; skip covercache.Force below
				case covercache.OverrideEmbedded:
					if data, ct, ok, pageErr := metadata.ExtractPDFPageCover(path, ov.Page); pageErr == nil && ok {
						coverBytes, coverContentType = data, ct
					} else {
						coverBytes = nil
					}
				}
			}

			if len(coverBytes) > 0 {
				if coverURL, err := covercache.Force(cfg.General.LogFolder, path, coverBytes, coverContentType); err == nil {
					b.CoverPath = coverURL
				}
			}

			cache.Put(path, librarycache.Entry{
				ModTime:         info.ModTime(),
				Size:            info.Size(),
				Title:           b.Title,
				Author:          b.Author,
				Year:            b.Year,
				Category:        b.Category,
				Subcategory:     b.Subcategory,
				CoverPath:       b.CoverPath,
				CoverOverridden: b.CoverOverridden,
				CoverVersion:    metadata.CoverExtractorVersion,
				MetadataVersion: metadata.MetadataExtractorVersion,
			})
		}

		books = append(books, b)
	}

	cache.Keep(seen)
	_ = cache.Save(cfg.General.LogFolder) // best-effort: a save failure shouldn't fail this Scan's results

	return books, nil
}
