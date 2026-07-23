// Package librarian walks the already-organized library folder
// (cfg.General.LibraryFolder) and reports what's in it, grouped by the
// Category/Subcategory folder structure rename.BuildPath already produces.
// Unlike internal/pipeline, it never computes a destination or moves
// anything -- it only reads back what's already there. Title/Author/Year
// and cover art are re-derived on every call via metadata.Extract, the same
// "never persisted" convention pipeline.Run uses for the working folder.
package librarian

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/FrancisChung/book-organiser/internal/config"
	"github.com/FrancisChung/book-organiser/internal/covercache"
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

// Scan walks cfg.General.LibraryFolder for every supported ebook file,
// deriving each book's Category/Subcategory from its position in the
// <library>/<Category>/<Subcategory>/<file> layout rename.BuildPath
// produces, and Title/Author/Year/cover art via metadata.Extract. A file
// sitting directly in <library>/ (no Category folder) or in
// <library>/<Category>/ with no Subcategory folder gets an empty
// Subcategory (and, for the former, an empty Category too) rather than
// being skipped -- Scan reports what it finds, it doesn't enforce layout.
// A file metadata.Extract fails on (e.g. corrupt) still gets a Book entry
// with empty Title/Author/Year/CoverPath rather than being dropped, so it's
// still visible on its shelf.
func Scan(cfg config.Config) ([]Book, error) {
	paths, err := scanner.Scan(cfg.General.LibraryFolder)
	if err != nil {
		return nil, err
	}

	books := make([]Book, 0, len(paths))
	for _, path := range paths {
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

		if res, err := metadata.Extract(path, cfg.TitleFormatting.HyphenExceptions, cfg.General.PDFCoverPageLimit); err == nil {
			b.Title = res.Title
			b.Author = res.Author
			b.Year = res.Year

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
					coverBytes = nil // already have a stable URL; skip covercache.Ensure below
				case covercache.OverrideEmbedded:
					if data, ct, ok, pageErr := metadata.ExtractPDFPageCover(path, ov.Page); pageErr == nil && ok {
						coverBytes, coverContentType = data, ct
					} else {
						coverBytes = nil
					}
				}
			}

			if len(coverBytes) > 0 {
				if coverURL, err := covercache.Ensure(cfg.General.LogFolder, path, statModTime(path), coverBytes, coverContentType); err == nil {
					b.CoverPath = coverURL
				}
			}
		}

		books = append(books, b)
	}
	return books, nil
}

func statModTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}
