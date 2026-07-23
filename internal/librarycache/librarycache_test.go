package librarycache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad_MissingFileReturnsEmptyCache(t *testing.T) {
	c := Load(t.TempDir())
	if _, ok := c.Fresh("/some/path.epub", time.Now(), 100); ok {
		t.Error("Fresh() = true for empty cache, want false")
	}
}

func TestLoad_CorruptFileReturnsEmptyCache(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "library-cache.json"), []byte("not json"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	c := Load(dir)
	if _, ok := c.Fresh("/some/path.epub", time.Now(), 100); ok {
		t.Error("Fresh() = true for corrupt cache, want false")
	}
}

func TestPutThenFresh_MatchingModTimeAndSizeIsFresh(t *testing.T) {
	var c Cache
	modTime := time.Now().Truncate(time.Second)
	c.Put("/book.epub", Entry{ModTime: modTime, Size: 100, Title: "Foundation"})

	entry, ok := c.Fresh("/book.epub", modTime, 100)
	if !ok {
		t.Fatal("Fresh() = false, want true")
	}
	if entry.Title != "Foundation" {
		t.Errorf("Title = %q, want Foundation", entry.Title)
	}
}

func TestFresh_DifferentModTimeIsStale(t *testing.T) {
	var c Cache
	modTime := time.Now().Truncate(time.Second)
	c.Put("/book.epub", Entry{ModTime: modTime, Size: 100})

	if _, ok := c.Fresh("/book.epub", modTime.Add(time.Hour), 100); ok {
		t.Error("Fresh() = true for a changed modTime, want false")
	}
}

func TestFresh_DifferentSizeIsStale(t *testing.T) {
	var c Cache
	modTime := time.Now().Truncate(time.Second)
	c.Put("/book.epub", Entry{ModTime: modTime, Size: 100})

	if _, ok := c.Fresh("/book.epub", modTime, 200); ok {
		t.Error("Fresh() = true for a changed size, want false")
	}
}

func TestKeep_DropsEntriesNotInSeen(t *testing.T) {
	var c Cache
	c.Put("/a.epub", Entry{Title: "A"})
	c.Put("/b.epub", Entry{Title: "B"})

	c.Keep(map[string]bool{"/a.epub": true})

	if _, ok := c.Fresh("/a.epub", time.Time{}, 0); !ok {
		t.Error("Fresh(/a.epub) = false after Keep, want true (still present)")
	}
	if _, ok := c.Fresh("/b.epub", time.Time{}, 0); ok {
		t.Error("Fresh(/b.epub) = true after Keep, want false (dropped)")
	}
}

func TestSaveThenLoad_RoundTripsEntriesIncludingCoverOverridden(t *testing.T) {
	dir := t.TempDir()
	modTime := time.Now().Truncate(time.Second)

	var c Cache
	c.Put("/book.epub", Entry{
		ModTime: modTime, Size: 100,
		Title: "Foundation", Author: "Isaac Asimov", Year: "1951",
		Category: "Fiction", Subcategory: "Sci-Fi", CoverPath: "/covers/abc.jpg",
		CoverOverridden: true,
	})
	if err := c.Save(dir); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	loaded := Load(dir)
	entry, ok := loaded.Fresh("/book.epub", modTime, 100)
	if !ok {
		t.Fatal("Fresh() = false after round-trip, want true")
	}
	if entry.Title != "Foundation" || entry.Author != "Isaac Asimov" || entry.Year != "1951" ||
		entry.Category != "Fiction" || entry.Subcategory != "Sci-Fi" || entry.CoverPath != "/covers/abc.jpg" ||
		!entry.CoverOverridden {
		t.Errorf("entry = %+v, want all fields round-tripped incl. CoverOverridden=true", entry)
	}
}

func TestSave_NoOpWhenNotDirty(t *testing.T) {
	dir := t.TempDir()
	c := Load(dir) // empty, not dirty
	if err := c.Save(dir); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "library-cache.json")); !os.IsNotExist(err) {
		t.Error("Save wrote a file for an unchanged empty cache, want no-op")
	}
}

func TestDelete_RemovesEntryAndMarksDirty(t *testing.T) {
	var c Cache
	c.Put("/book.epub", Entry{Title: "Foundation"})
	c.dirty = false // simulate a freshly-Saved, clean state before Delete

	c.Delete("/book.epub")

	if _, ok := c.Fresh("/book.epub", time.Time{}, 0); ok {
		t.Error("Fresh() = true after Delete, want false")
	}
	if !c.Dirty() {
		t.Error("Dirty() = false after Delete, want true")
	}
}

func TestDelete_OfAbsentPathStillMarksDirty(t *testing.T) {
	var c Cache
	c.Delete("/never-cached.epub")
	if !c.Dirty() {
		t.Error("Dirty() = false after Delete of an absent path, want true (Save must still be attempted)")
	}
}

func TestInvalidate_RemovesPersistedEntry(t *testing.T) {
	dir := t.TempDir()
	modTime := time.Now().Truncate(time.Second)

	var c Cache
	c.Put("/book.epub", Entry{ModTime: modTime, Size: 100, Title: "Foundation"})
	if err := c.Save(dir); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	if err := Invalidate(dir, "/book.epub"); err != nil {
		t.Fatalf("Invalidate returned error: %v", err)
	}

	reloaded := Load(dir)
	if _, ok := reloaded.Fresh("/book.epub", modTime, 100); ok {
		t.Error("Fresh() = true after Invalidate, want false (entry was removed and the removal persisted)")
	}
}

func TestInvalidate_PropagatesSaveFailure(t *testing.T) {
	dir := t.TempDir()
	// Make the cache file's own path a directory instead of a writable
	// file, so Cache.Save's os.WriteFile fails with EISDIR -- proving
	// Invalidate surfaces a real I/O failure rather than swallowing it.
	if err := os.MkdirAll(filepath.Join(dir, "library-cache.json"), 0755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}

	if err := Invalidate(dir, "/book.epub"); err == nil {
		t.Error("Invalidate returned nil error, want an error from the blocked write")
	}
}
