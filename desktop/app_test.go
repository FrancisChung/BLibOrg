package main

import (
	"path/filepath"
	"testing"
)

func TestIsAffirmative(t *testing.T) {
	tests := []struct {
		name   string
		result string
		want   bool
	}{
		{"macOS custom button label", "Move files", true},
		{"macOS custom undo button label", "Undo", true},
		{"Linux/Windows default Yes (Buttons option is ignored there)", "Yes", true},
		{"generic OK fallback", "OK", true},
		{"macOS custom cancel label", "Cancel", false},
		{"Linux/Windows default No", "No", false},
		{"dialog dismissed with no selection", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAffirmative(tt.result); got != tt.want {
				t.Errorf("isAffirmative(%q) = %v, want %v", tt.result, got, tt.want)
			}
		})
	}
}

func TestFileURI_EscapesSpacesAndParens(t *testing.T) {
	got := fileURI("/library/Atomic Kotlin (2021) - Bruce Eckel.epub")
	want := "file:///library/Atomic%20Kotlin%20%282021%29%20-%20Bruce%20Eckel.epub"
	if got != want {
		t.Errorf("fileURI() = %q, want %q", got, want)
	}
}

func TestOpenFile_NonExistentFileReturnsError(t *testing.T) {
	// Only the non-existent-file path is safe to unit test: OpenFile
	// returns before ever reaching runtime.BrowserOpenURL, which calls
	// log.Fatal (terminating the process, not a recoverable panic) when
	// ctx lacks a real Wails frontend value -- as a bare NewApp() always
	// does outside a live Wails runtime. This matches the existing
	// precedent set by ConfirmApply/ConfirmUndo having no direct tests
	// for the same reason.
	app := NewApp()
	err := app.OpenFile(filepath.Join(t.TempDir(), "does-not-exist.epub"))
	if err == nil {
		t.Fatal("expected an error for a nonexistent file, got nil")
	}
}
