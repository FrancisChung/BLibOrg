// Package covercache writes extracted book-cover images to a stable,
// path-addressed location on disk (keyed by the book's source path, not the
// image bytes) so the frontend can load them via a
// plain same-origin URL. Wails 2.13 blocks file:// URLs in its webview (see
// desktop/app.go's OpenFile doc comment), so covers can't be passed to the
// frontend as raw filesystem paths; caching them under the app's existing
// LogFolder and serving them at a fixed URL prefix (see desktop/covers.go,
// Task 7) sidesteps that instead of re-sending base64 image data on every
// library listing.
package covercache

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

var extByContentType = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/gif":  ".gif",
}

// Dir returns the cache directory under logFolder that cover files live in.
func Dir(logFolder string) string {
	return filepath.Join(logFolder, "covers")
}

// fileName returns the cache filename for sourcePath: a hash of the path
// itself (not its bytes -- one book, one stable name across calls, so the
// served URL doesn't change on every relist), an optional suffix (used by
// WriteCandidateImage to distinguish one page from another for the same
// book), plus an extension inferred from contentType.
func fileName(sourcePath, suffix, contentType string) string {
	sum := sha256.Sum256([]byte(sourcePath))
	ext := extByContentType[contentType]
	if ext == "" {
		ext = ".img"
	}
	return hex.EncodeToString(sum[:]) + suffix + ext
}

// writeCoverFile (re)writes bytes to name under logFolder's cover cache
// directory, creating the directory if needed, and returns the URL path
// the frontend should use as an <img src>.
func writeCoverFile(logFolder, name string, bytes []byte) (string, error) {
	dir := Dir(logFolder)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, name), bytes, 0644); err != nil {
		return "", err
	}
	return "/covers/" + name, nil
}

// Ensure writes coverBytes to the cache under logFolder if no fresher cache
// entry already exists for sourcePath, and returns the URL path the
// frontend should use as an <img src> (served by desktop/covers.go's
// coverHandler). Returns "" if coverBytes is empty -- the caller found no
// extractable cover for this book. sourceModTime is the book file's own
// mtime; an existing cache file at least that new is reused as-is rather
// than rewritten, so a re-scan of an unchanged library doesn't redo image
// writes on every call.
func Ensure(logFolder, sourcePath string, sourceModTime time.Time, coverBytes []byte, contentType string) (string, error) {
	if len(coverBytes) == 0 {
		return "", nil
	}
	name := fileName(sourcePath, "", contentType)
	cachePath := filepath.Join(Dir(logFolder), name)

	if info, err := os.Stat(cachePath); err == nil && !info.ModTime().Before(sourceModTime) {
		return "/covers/" + name, nil
	}

	return writeCoverFile(logFolder, name, coverBytes)
}

// Force writes coverBytes to the cache unconditionally, unlike Ensure
// (which reuses an existing cache file at least as new as
// sourceModTime). Used by manual cover-override set/clear: the source
// file's own mtime hasn't changed, but the served URL must reflect the
// new choice immediately rather than waiting for the next time the
// source file itself changes.
func Force(logFolder, sourcePath string, coverBytes []byte, contentType string) (string, error) {
	if len(coverBytes) == 0 {
		return "", nil
	}
	return writeCoverFile(logFolder, fileName(sourcePath, "", contentType), coverBytes)
}

// WriteCustomOverrideImage stores a user-uploaded cover image under the
// same covercache.Dir as auto-detected covers (rather than the design
// doc's separate covers/overrides/ subdirectory -- see this plan's Global
// Constraints for why), with an "override-" filename prefix so it can't
// collide with sourcePath's own auto-cache entry. Always (re)writes:
// there's no "is this already cached" question for a fresh upload.
func WriteCustomOverrideImage(logFolder, sourcePath string, imageBytes []byte, contentType string) (string, error) {
	return writeCoverFile(logFolder, "override-"+fileName(sourcePath, "", contentType), imageBytes)
}

// WriteCandidateImage stores one page's candidate cover image (from
// metadata.ListPDFCoverCandidates) for the cover-picker's thumbnail grid,
// under covercache.Dir with a "candidate-" prefix (see
// WriteCustomOverrideImage's doc comment for why this dir, not a
// candidates/ subdirectory), and a "-p<page>" suffix (via fileName) so the
// grid gets one stable, distinct URL per (book, page) pair across calls.
func WriteCandidateImage(logFolder, sourcePath string, page int, imageBytes []byte, contentType string) (string, error) {
	name := "candidate-" + fileName(sourcePath, "-p"+strconv.Itoa(page), contentType)
	return writeCoverFile(logFolder, name, imageBytes)
}
