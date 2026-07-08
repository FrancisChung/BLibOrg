package scanner

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestScan(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "subdir")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	files := []string{
		filepath.Join(root, "book1.epub"),
		filepath.Join(root, "book2.PDF"), // uppercase extension
		filepath.Join(sub, "book3.mobi"),
		filepath.Join(sub, "book4.azw3"),
		filepath.Join(root, "notes.txt"),   // unsupported
		filepath.Join(root, "cover.cbz"),   // unsupported
	}
	for _, f := range files {
		if err := os.WriteFile(f, []byte("x"), 0644); err != nil {
			t.Fatalf("write %s: %v", f, err)
		}
	}

	got, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	sort.Strings(got)

	want := []string{
		filepath.Join(root, "book1.epub"),
		filepath.Join(root, "book2.PDF"),
		filepath.Join(sub, "book3.mobi"),
		filepath.Join(sub, "book4.azw3"),
	}
	sort.Strings(want)

	if len(got) != len(want) {
		t.Fatalf("Scan() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Scan()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
