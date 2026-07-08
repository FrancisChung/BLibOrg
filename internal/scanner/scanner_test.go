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

func TestScan_SkipsUnreadableSubdirButFindsOtherBooks(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("permission checks don't apply when running as root")
	}

	root := t.TempDir()
	good := filepath.Join(root, "good.epub")
	if err := os.WriteFile(good, []byte("x"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	blocked := filepath.Join(root, "blocked")
	if err := os.MkdirAll(blocked, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	hidden := filepath.Join(blocked, "hidden.epub")
	if err := os.WriteFile(hidden, []byte("x"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.Chmod(blocked, 0000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(blocked, 0755) }) // allow TempDir cleanup to remove it

	got, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan returned error: %v, want nil (should skip unreadable subdir, not fail)", err)
	}
	if len(got) != 1 || got[0] != good {
		t.Errorf("Scan() = %v, want [%s] (blocked subdir's book should be skipped, not cause total failure)", got, good)
	}
}

func TestScan_ReturnsErrorWhenRootItselfIsInaccessible(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("permission checks don't apply when running as root")
	}

	parent := t.TempDir()
	root := filepath.Join(parent, "inaccessible-root")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(root, 0000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(root, 0755) }) // allow TempDir cleanup to remove it

	_, err := Scan(root)
	if err == nil {
		t.Error("Scan(root) with an inaccessible root folder returned nil error, want a fatal error")
	}
}
