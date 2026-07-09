// Package pipeline wires the scanner, metadata, heuristics, categorizer,
// rename, and duplicates packages together into a single read-only
// scan/preview entry point.
package pipeline

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/FrancisChung/book-organiser/internal/book"
	"github.com/FrancisChung/book-organiser/internal/categorizer"
	"github.com/FrancisChung/book-organiser/internal/config"
	"github.com/FrancisChung/book-organiser/internal/duplicates"
	"github.com/FrancisChung/book-organiser/internal/heuristics"
	"github.com/FrancisChung/book-organiser/internal/metadata"
	"github.com/FrancisChung/book-organiser/internal/rename"
	"github.com/FrancisChung/book-organiser/internal/scanner"
)

// Run scans cfg.General.WorkingFolder, resolves metadata (embedded first,
// filename heuristics as fallback), categorizes, computes destination
// paths, and flags likely duplicates. It performs no file moves -- this is
// the read-only "preview" stage that View 1 / View 2 render; applying the
// resulting DestPath values is a separate step via the operations package.
func Run(cfg config.Config) ([]*book.Book, error) {
	paths, err := scanner.Scan(cfg.General.WorkingFolder)
	if err != nil {
		return nil, err
	}

	books := make([]*book.Book, 0, len(paths))
	for _, path := range paths {
		b := &book.Book{
			SourcePath: path,
			Format:     strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
		}
		if info, err := os.Stat(path); err == nil {
			b.SizeBytes = info.Size()
		}

		if res, err := metadata.Extract(path); err == nil {
			if res.Title != "" {
				b.Title = book.Field{Value: res.Title, Source: book.SourceMetadata}
			}
			if res.Author != "" {
				b.Author = book.Field{Value: res.Author, Source: book.SourceMetadata}
			}
			if res.Year != "" {
				b.Year = book.Field{Value: res.Year, Source: book.SourceMetadata}
			}
			b.Subject = res.Subject
		}

		if b.Title.Value == "" || b.Author.Value == "" || b.Year.Value == "" {
			stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			h := heuristics.Parse(stem, cfg.Heuristics.KnownJunkTags)
			if b.Title.Value == "" && h.Title != "" {
				b.Title = book.Field{Value: h.Title, Source: book.SourceHeuristic}
			}
			if b.Author.Value == "" && h.Author != "" {
				b.Author = book.Field{Value: h.Author, Source: book.SourceHeuristic}
			}
			if b.Year.Value == "" && h.Year != "" {
				b.Year = book.Field{Value: h.Year, Source: book.SourceHeuristic}
			}
		}

		categorizer.Categorize(b, cfg)
		rename.BuildPath(b, cfg)
		books = append(books, b)
	}

	duplicates.Detect(books)
	return books, nil
}
