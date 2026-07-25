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
	"runtime"
	"strings"
	"sync"

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
// a Book entry (with an empty CoverPath, and Title/Author/Year filled in
// on a best-effort basis by applyFilenameHeuristicFallback) rather than
// being dropped, so it's still visible on its shelf; such a file is never
// cached, so it's retried on every subsequent Scan until it succeeds or is
// removed.
//
// Every path's single-book work (scanOneBook, below) runs concurrently,
// bounded by cfg.General.ScanConcurrency (0/unset means
// runtime.NumCPU()) via a semaphore channel + WaitGroup -- safe because
// metadata.Extract is documented safe for concurrent use (see its own
// doc comment) and the one other piece of state shared across workers,
// the in-memory cache, is guarded by its own mutex (see scanOneBook).
// The returned slice preserves paths' original order regardless of which
// goroutine finishes first, matching the pre-parallel behavior exactly.
func Scan(cfg config.Config, forceRefresh bool) ([]Book, error) {
	paths, err := scanner.Scan(cfg.General.LibraryFolder)
	if err != nil {
		return nil, err
	}

	cache := librarycache.Load(cfg.General.LogFolder)
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		seen[path] = true
	}

	concurrency := cfg.General.ScanConcurrency
	if concurrency <= 0 {
		concurrency = runtime.NumCPU()
	}

	results := make([]Book, len(paths))
	included := make([]bool, len(paths))
	var cacheMu sync.Mutex
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, path := range paths {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, path string) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i], included[i] = scanOneBook(cfg, forceRefresh, &cache, &cacheMu, path)
		}(i, path)
	}
	wg.Wait()

	books := make([]Book, 0, len(paths))
	for i, b := range results {
		if included[i] {
			books = append(books, b)
		}
	}

	cache.Keep(seen)
	_ = cache.Save(cfg.General.LogFolder) // best-effort: a save failure shouldn't fail this Scan's results

	return books, nil
}

// scanOneBook resolves a single book at path -- its Category/Subcategory
// from its position under cfg.General.LibraryFolder, then either a cache
// hit or a full metadata.Extract-then-override-check -- the single-book
// logic Scan's loop used to run inline, extracted so a bounded pool of
// goroutines can each run one call of this function concurrently. ok is
// false only when path can't be made relative to
// cfg.General.LibraryFolder (mirrors the original loop's "continue"
// entirely skipping that path, rather than adding an empty entry for
// it). cache and cacheMu are shared across every concurrent call: cacheMu
// must be held around every cache.Fresh/cache.Put call (librarycache.Cache
// wraps a plain, non-thread-safe map), but never around the expensive
// metadata.Extract/covercache work in between -- concurrent extraction is
// what actually gets parallelized, not serialized behind the same lock.
func scanOneBook(cfg config.Config, forceRefresh bool, cache *librarycache.Cache, cacheMu *sync.Mutex, path string) (Book, bool) {
	rel, err := filepath.Rel(cfg.General.LibraryFolder, path)
	if err != nil {
		return Book{}, false
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
		return b, true
	}

	if !forceRefresh {
		cacheMu.Lock()
		entry, ok := cache.Fresh(path, info.ModTime(), info.Size())
		cacheMu.Unlock()
		if ok && entry.CoverVersion == metadata.CoverExtractorVersion && entry.MetadataVersion == metadata.MetadataExtractorVersion {
			b.Title = entry.Title
			b.Author = entry.Author
			b.Year = entry.Year
			b.CoverPath = entry.CoverPath
			b.CoverOverridden = entry.CoverOverridden
			return b, true
		}
	}

	if res, err := extractFunc(path, cfg.TitleFormatting.HyphenExceptions, cfg.General.PDFCoverPageLimit); err == nil {
		b.Title = res.Title
		b.Author = res.Author
		b.Year = res.Year

		applyFilenameHeuristicFallback(&b, path, cfg.Heuristics.KnownJunkTags)

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

		cacheMu.Lock()
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
		cacheMu.Unlock()
	} else {
		// extractFunc failed (e.g. a corrupt file) -- this book is never
		// cached (see Scan's doc comment), so it gets no cache-hit
		// benefit from the fallback below, but it should still get a
		// best-effort Title/Author/Year rather than staying blank until
		// the frontend's raw-filename fallback takes over.
		applyFilenameHeuristicFallback(&b, path, cfg.Heuristics.KnownJunkTags)
	}

	return b, true
}

// applyFilenameHeuristicFallback fills in any of b's Title/Author/Year
// that came back empty (a missing field, or extractEpub/extractPDF's own
// placeholder-value checks blanking a known-junk value) using
// heuristics.Parse against path's filename -- mirroring
// internal/pipeline.Run's existing filename-heuristic fallback, which the
// Library view previously had no equivalent of at all. A non-empty
// metadata-sourced field is never overwritten. Called from both the
// extraction-success and extraction-failure branches of Scan: on success,
// this must run before cache.Put so the heuristic-derived values -- not
// blanks -- are what gets cached (otherwise the very next Scan would read
// the blank cached entry back on a cache hit, silently reverting this
// fallback); on failure, the book is never cached at all, so this simply
// gives it a best-effort Title/Author/Year instead of leaving it blank
// until the frontend's own raw-filename display fallback takes over.
func applyFilenameHeuristicFallback(b *Book, path string, knownJunkTags []string) {
	if b.Title != "" && b.Author != "" && b.Year != "" {
		return
	}
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	h := heuristics.Parse(stem, knownJunkTags)
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
