// Package librarycache persists internal/librarian.Scan's derived,
// already override-resolved per-book fields
// (Title/Author/Year/Category/Subcategory/CoverPath/CoverOverridden) keyed
// by source path, so a Scan of an unchanged library can skip the expensive
// metadata.Extract/covercache.Ensure/covercache.GetOverride trio entirely
// for files whose ModTime and Size haven't changed since they were last
// cached. Because the stored CoverPath/CoverOverridden are already
// override-resolved (not the "raw" auto-detected result), any cover
// override change (internal/appapi's SetCoverOverride, SetCoverOverrideCustom,
// ClearCoverOverride) MUST call Invalidate for the affected path -- a cache
// hit is never re-checked against the override store, so a missed
// invalidation would silently serve a stale cover indefinitely.
package librarycache

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Entry is one cached book's derived fields, valid as long as the source
// file's ModTime and Size match what was recorded when it was cached.
// CoverVersion records which metadata.CoverExtractorVersion produced
// CoverPath -- callers (internal/librarian.Scan) are responsible for
// comparing it against the current version, since a ModTime+Size match
// alone can't tell "the book file is unchanged" apart from "the cover
// extraction algorithm has since improved and would now find something
// different." A zero value (the Go default, and what any Entry persisted
// before this field existed unmarshals to) is deliberately never a valid
// version, so pre-existing entries safely miss once and self-heal.
type Entry struct {
	ModTime         time.Time `json:"modTime"`
	Size            int64     `json:"size"`
	Title           string    `json:"title"`
	Author          string    `json:"author"`
	Year            string    `json:"year"`
	Category        string    `json:"category"`
	Subcategory     string    `json:"subcategory"`
	CoverPath       string    `json:"coverPath"`
	CoverOverridden bool      `json:"coverOverridden"`
	CoverVersion    int       `json:"coverVersion"`
	MetadataVersion int       `json:"metadataVersion"`
}

// Cache is an in-memory, path-keyed view of the persisted library scan
// cache. The zero value is a valid empty cache.
type Cache struct {
	entries map[string]Entry
	dirty   bool
}

const cacheFileName = "library-cache.json"

func cachePath(logFolder string) string {
	return filepath.Join(logFolder, cacheFileName)
}

// Load reads the cache file under logFolder. A missing or corrupt file
// returns an empty, valid Cache rather than an error -- the cache is purely
// an optimization; losing it just means the next Scan re-extracts
// everything, the same as having no cache at all.
func Load(logFolder string) Cache {
	data, err := os.ReadFile(cachePath(logFolder))
	if err != nil {
		return Cache{entries: map[string]Entry{}}
	}
	var entries map[string]Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return Cache{entries: map[string]Entry{}}
	}
	if entries == nil {
		entries = map[string]Entry{}
	}
	return Cache{entries: entries}
}

// Fresh returns the cached entry for sourcePath and whether it's still
// valid for a file with the given current modTime and size. A cache miss,
// or a modTime/size mismatch (the file was edited), both report ok=false.
func (c Cache) Fresh(sourcePath string, modTime time.Time, size int64) (Entry, bool) {
	entry, found := c.entries[sourcePath]
	if !found || !entry.ModTime.Equal(modTime) || entry.Size != size {
		return Entry{}, false
	}
	return entry, true
}

// Put records or replaces the cached entry for sourcePath.
func (c *Cache) Put(sourcePath string, entry Entry) {
	if c.entries == nil {
		c.entries = map[string]Entry{}
	}
	c.entries[sourcePath] = entry
	c.dirty = true
}

// Delete removes sourcePath's cached entry, if any, and unconditionally
// marks the cache dirty -- even when sourcePath had no entry to begin
// with. This is deliberate: Delete backs cover-override invalidation
// (Invalidate, below), and the caller needs Save to actually attempt a
// write (and so report any I/O failure) regardless of whether this
// specific path happened to be cached yet.
func (c *Cache) Delete(sourcePath string) {
	delete(c.entries, sourcePath)
	c.dirty = true
}

// Keep drops every cached entry whose path is not in seen, so files
// deleted or moved out of the library folder since the last scan don't
// linger in the saved cache forever.
func (c *Cache) Keep(seen map[string]bool) {
	for path := range c.entries {
		if !seen[path] {
			delete(c.entries, path)
			c.dirty = true
		}
	}
}

// Dirty reports whether the cache has unsaved changes since it was loaded
// (or since the last Save).
func (c Cache) Dirty() bool {
	return c.dirty
}

// Save writes the cache to logFolder if it has unsaved changes; a no-op
// otherwise, so an all-cache-hit Scan doesn't rewrite the file every time.
func (c *Cache) Save(logFolder string) error {
	if !c.dirty {
		return nil
	}
	data, err := json.Marshal(c.entries)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(logFolder, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(cachePath(logFolder), data, 0644); err != nil {
		return err
	}
	c.dirty = false
	return nil
}

// Invalidate is the Load-Delete-Save round trip cover-override changes use
// to drop one book's cached entry outside of a Scan call, forcing the next
// Scan to treat that file as a miss and re-resolve it (which re-checks the
// override store, so it picks up the just-changed override correctly).
func Invalidate(logFolder, sourcePath string) error {
	c := Load(logFolder)
	c.Delete(sourcePath)
	return c.Save(logFolder)
}

// Reset deletes the persisted cache file entirely, forcing every book to
// be treated as new on the next Scan. A missing file is not an error --
// idempotent, matching Load's own fail-open convention.
func Reset(logFolder string) error {
	err := os.Remove(cachePath(logFolder))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
