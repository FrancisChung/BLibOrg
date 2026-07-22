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
// served URL doesn't change on every relist) plus an extension inferred
// from contentType.
func fileName(sourcePath, contentType string) string {
	sum := sha256.Sum256([]byte(sourcePath))
	ext := extByContentType[contentType]
	if ext == "" {
		ext = ".img"
	}
	return hex.EncodeToString(sum[:]) + ext
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
	dir := Dir(logFolder)
	name := fileName(sourcePath, contentType)
	cachePath := filepath.Join(dir, name)

	if info, err := os.Stat(cachePath); err == nil && !info.ModTime().Before(sourceModTime) {
		return "/covers/" + name, nil
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(cachePath, coverBytes, 0644); err != nil {
		return "", err
	}
	return "/covers/" + name, nil
}
