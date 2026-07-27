package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
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
		{"macOS custom reset button label", "Reset", true},
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

func TestNewScanProgressEmitter_NilContextReturnsNil(t *testing.T) {
	if got := newScanProgressEmitter(nil); got != nil {
		t.Error("newScanProgressEmitter(nil) should return nil")
	}
}

func TestNewScanProgressEmitter_NonNilContextReturnsCallback(t *testing.T) {
	if got := newScanProgressEmitter(context.Background()); got == nil {
		t.Error("newScanProgressEmitter(context.Background()) should return a non-nil callback")
	}
}

// TestAppDTS_AllDeclaredFunctionsHaveExportedMethods guards against the
// hand-maintained desktop/frontend/wailsjs/go/main/App.d.ts drifting out of
// sync with *App: every JS binding it declares must have a matching
// exported method on *App, or the frontend call is `undefined` at runtime
// (window['go']['main']['App']['Foo'] doesn't exist) even though it
// type-checks in TypeScript and everything else in the stack looks wired
// up. This exact bug shipped for GetScanConcurrency/SetScanConcurrency:
// appapi.App had the methods, App.d.ts had the JS declarations, but
// desktop/app.go had no forwarders, so Wails never bound them.
func TestAppDTS_AllDeclaredFunctionsHaveExportedMethods(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not determine this test file's own path via runtime.Caller")
	}
	dtsPath := filepath.Join(filepath.Dir(thisFile), "frontend", "wailsjs", "go", "main", "App.d.ts")

	data, err := os.ReadFile(dtsPath)
	if err != nil {
		t.Fatalf("reading %s: %v", dtsPath, err)
	}

	re := regexp.MustCompile(`(?m)^export function (\w+)\(`)
	matches := re.FindAllStringSubmatch(string(data), -1)
	if len(matches) == 0 {
		t.Fatalf("found no `export function` declarations in %s -- regex may be broken, or the generated file's format changed", dtsPath)
	}

	appType := reflect.TypeOf(&App{})
	for _, m := range matches {
		name := m[1]
		t.Run(name, func(t *testing.T) {
			if _, ok := appType.MethodByName(name); !ok {
				t.Errorf("App.d.ts declares %s(), but *App has no exported method of that name -- "+
					"add a forwarder in desktop/app.go or the frontend call will be undefined at runtime", name)
			}
		})
	}
}
