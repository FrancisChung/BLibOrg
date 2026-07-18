package main

import (
	"path/filepath"
	"runtime"
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

func TestOpenCommand_PicksPlatformOpener(t *testing.T) {
	path := "/library/Atomic Kotlin (2021) - Bruce Eckel.epub"
	name, args := openCommand(path)

	var wantName string
	var wantArgs []string
	switch runtime.GOOS {
	case "darwin":
		wantName, wantArgs = "open", []string{path}
	case "windows":
		wantName, wantArgs = "rundll32", []string{"url.dll,FileProtocolHandler", path}
	default:
		wantName, wantArgs = "xdg-open", []string{path}
	}

	if name != wantName || len(args) != len(wantArgs) || args[len(args)-1] != wantArgs[len(wantArgs)-1] {
		t.Errorf("openCommand(%q) = %q, %q, want %q, %q", path, name, args, wantName, wantArgs)
	}
}

func TestOpenFile_NonExistentFileReturnsError(t *testing.T) {
	// Only the non-existent-file path is safe to unit test here: for an
	// existing file, OpenFile actually launches the platform opener
	// (xdg-open/open/rundll32), which would pop open a real application
	// on whatever machine runs the test suite.
	app := NewApp()
	err := app.OpenFile(filepath.Join(t.TempDir(), "does-not-exist.epub"))
	if err == nil {
		t.Fatal("expected an error for a nonexistent file, got nil")
	}
}
