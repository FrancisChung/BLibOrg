// Package pipeline wires the scanner, metadata, heuristics, categorizer,
// rename, and duplicates packages together into a single read-only
// scan/preview entry point.
package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/FrancisChung/BLibOrg/internal/book"
	"github.com/FrancisChung/BLibOrg/internal/categorizer"
	"github.com/FrancisChung/BLibOrg/internal/config"
	"github.com/FrancisChung/BLibOrg/internal/duplicates"
	"github.com/FrancisChung/BLibOrg/internal/heuristics"
	"github.com/FrancisChung/BLibOrg/internal/metadata"
	"github.com/FrancisChung/BLibOrg/internal/rename"
	"github.com/FrancisChung/BLibOrg/internal/scanner"
)

// Run scans cfg.General.WorkingFolder, resolves metadata (embedded first,
// filename heuristics as fallback), categorizes, computes destination
// paths, and flags likely duplicates. It performs no file moves -- this is
// the read-only "preview" stage that View 1 / View 2 render; applying the
// resulting DestPath values is a separate step via the operations package.
func Run(cfg config.Config) ([]*book.Book, error) {
	return RunWithProgress(cfg, nil)
}

// RunWithProgress is Run with an optional callback invoked once per book as
// its extraction finishes. The returned order remains scanner order.
func RunWithProgress(cfg config.Config, onProgress func(done, total int)) ([]*book.Book, error) {
	paths, err := scanner.Scan(cfg.General.WorkingFolder)
	if err != nil {
		return nil, err
	}

	results := make([]*book.Book, len(paths))
	concurrency := cfg.General.ScanConcurrency
	if concurrency <= 0 {
		concurrency = runtime.NumCPU()
	}
	if concurrency < 1 {
		concurrency = 1
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var progressMu sync.Mutex
	done := 0
	if onProgress != nil {
		onProgress(0, len(paths))
	}
	for i, path := range paths {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, path string) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = scanOneBook(cfg, path)
			if onProgress != nil {
				progressMu.Lock()
				done++
				onProgress(done, len(paths))
				progressMu.Unlock()
			}
		}(i, path)
	}
	wg.Wait()

	books := make([]*book.Book, 0, len(results))
	for _, b := range results {
		if b != nil {
			books = append(books, b)
		}
	}

	disambiguateDestPaths(books)
	duplicates.Detect(books)
	return books, nil
}

func scanOneBook(cfg config.Config, path string) *book.Book {
	b := &book.Book{
		SourcePath: path,
		Format:     strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
	}
	if info, err := os.Stat(path); err == nil {
		b.SizeBytes = info.Size()
	}

	if res, err := metadata.ExtractMetadata(path, cfg.TitleFormatting.HyphenExceptions, cfg.General.PDFCoverPageLimit); err == nil {
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
	return b
}

// disambiguateDestPaths finds books that ended up with an identical
// DestPath (e.g. two different books whose title+author+category happen to
// render the same filename) and appends a " (2)", " (3)", ... suffix
// before the file extension to all but the first occurrence, in slice
// order, so every book has a unique DestPath by the time Run returns.
// Without this, applying the resulting batch via operations.Manager would
// have the second move collide with the first -- refused outright since
// the MoveCommand existence-check fix, but still a confusing batch failure
// rather than a clean preview. This is intentionally simple: stable
// slice-order numbering, no attempt to detect a newly-suffixed path
// colliding with a later book's own naturally-suffixed name.
func disambiguateDestPaths(books []*book.Book) {
	seen := make(map[string]int, len(books))
	for _, b := range books {
		if b.DestPath == "" {
			continue
		}
		n := seen[b.DestPath]
		seen[b.DestPath] = n + 1
		if n == 0 {
			continue
		}
		ext := filepath.Ext(b.DestPath)
		base := strings.TrimSuffix(b.DestPath, ext)
		b.DestPath = fmt.Sprintf("%s (%d)%s", base, n+1, ext)
	}
}
