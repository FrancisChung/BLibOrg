package covercache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetOverride_NoFileYetReturnsNotFound(t *testing.T) {
	logFolder := t.TempDir()
	_, found, err := GetOverride(logFolder, "/library/Fiction/book.pdf")
	if err != nil {
		t.Fatalf("GetOverride returned error: %v", err)
	}
	if found {
		t.Error("found = true, want false (no overrides file exists yet)")
	}
}

func TestSetOverride_ThenGetOverrideRoundTrips(t *testing.T) {
	logFolder := t.TempDir()
	sourcePath := "/library/Fiction/book.pdf"
	want := Override{Type: OverrideEmbedded, Page: 3}

	if err := SetOverride(logFolder, sourcePath, want); err != nil {
		t.Fatalf("SetOverride returned error: %v", err)
	}

	got, found, err := GetOverride(logFolder, sourcePath)
	if err != nil {
		t.Fatalf("GetOverride returned error: %v", err)
	}
	if !found {
		t.Fatal("found = false, want true")
	}
	if got != want {
		t.Errorf("got = %+v, want %+v", got, want)
	}
}

func TestSetOverride_PersistsAcrossSeparateCalls(t *testing.T) {
	logFolder := t.TempDir()
	if err := SetOverride(logFolder, "/library/a.pdf", Override{Type: OverrideEmbedded, Page: 1}); err != nil {
		t.Fatalf("SetOverride a: %v", err)
	}
	if err := SetOverride(logFolder, "/library/b.pdf", Override{Type: OverrideCustom, ImagePath: "/covers/override-xyz.jpg"}); err != nil {
		t.Fatalf("SetOverride b: %v", err)
	}

	a, found, err := GetOverride(logFolder, "/library/a.pdf")
	if err != nil || !found || a.Page != 1 {
		t.Errorf("GetOverride a = %+v, %v, %v, want Page=1, true, nil", a, found, err)
	}
	b, found, err := GetOverride(logFolder, "/library/b.pdf")
	if err != nil || !found || b.ImagePath != "/covers/override-xyz.jpg" {
		t.Errorf("GetOverride b = %+v, %v, %v", b, found, err)
	}
}

func TestClearOverride_RemovesEntry(t *testing.T) {
	logFolder := t.TempDir()
	sourcePath := "/library/Fiction/book.pdf"
	if err := SetOverride(logFolder, sourcePath, Override{Type: OverrideEmbedded, Page: 2}); err != nil {
		t.Fatalf("SetOverride: %v", err)
	}

	if err := ClearOverride(logFolder, sourcePath); err != nil {
		t.Fatalf("ClearOverride returned error: %v", err)
	}

	_, found, err := GetOverride(logFolder, sourcePath)
	if err != nil {
		t.Fatalf("GetOverride returned error: %v", err)
	}
	if found {
		t.Error("found = true, want false (override was cleared)")
	}
}

func TestClearOverride_NoFileYetIsANoOp(t *testing.T) {
	logFolder := t.TempDir()
	if err := ClearOverride(logFolder, "/library/never-set.pdf"); err != nil {
		t.Fatalf("ClearOverride returned error: %v, want nil (nothing to clear)", err)
	}
}

func TestSetOverride_WritesReadableJSONFile(t *testing.T) {
	logFolder := t.TempDir()
	if err := SetOverride(logFolder, "/library/book.pdf", Override{Type: OverrideEmbedded, Page: 1}); err != nil {
		t.Fatalf("SetOverride: %v", err)
	}
	path := filepath.Join(logFolder, "cover-overrides.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}
