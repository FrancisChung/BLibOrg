package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	osruntime "runtime"

	"github.com/FrancisChung/BLibOrg/internal/appapi"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the Wails-bound struct. It holds no business logic of its own --
// every method delegates to appapi.App, except ConfirmApply, which needs
// the Wails runtime context for a native dialog.
type App struct {
	ctx context.Context
	api *appapi.App
}

func NewApp() *App {
	return &App{api: appapi.NewApp()}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) Scan() ([]appapi.BookView, error) {
	return a.api.ScanWithProgress(newBookScanProgressEmitter(a.ctx))
}

// bookScanProgress is the JSON payload of the "scan:progress" event emitted
// while the unsorted-folder review scan is running.
type bookScanProgress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

func newBookScanProgressEmitter(ctx context.Context) func(done, total int) {
	if ctx == nil {
		return nil
	}
	return func(done, total int) {
		runtime.EventsEmit(ctx, "scan:progress", bookScanProgress{Done: done, Total: total})
	}
}

func (a *App) Recompute(edited appapi.BookView) (appapi.BookView, error) {
	return a.api.Recompute(edited)
}

func (a *App) Apply(books []appapi.BookView) (appapi.ApplyResult, error) {
	return a.api.Apply(books)
}

func (a *App) ConfigStatus() appapi.ConfigStatusView {
	return a.api.ConfigStatus()
}

func (a *App) ListOperationBatches() ([]appapi.OperationBatchView, error) {
	return a.api.ListOperationBatches()
}

func (a *App) ListCategoryWarnings() ([]appapi.CategoryWarningView, error) {
	return a.api.ListCategoryWarnings()
}

func (a *App) Categories() ([]appapi.DestinationView, error) {
	return a.api.Categories()
}

func (a *App) UndoBatch(batchID string) error {
	return a.api.UndoBatch(batchID)
}

// libraryScanProgress is the JSON payload of the "library:scan-progress"
// Wails event emitted while a Library refresh is running.
type libraryScanProgress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// newScanProgressEmitter returns a librarian.Scan-compatible onProgress
// callback that emits a "library:scan-progress" Wails event through ctx,
// or nil if ctx is nil. Wails' runtime.EventsEmit calls log.Fatalf (an
// os.Exit) if given a context that was never set up by Wails' own
// startup hook -- a.ctx is exactly such a context in any test that
// constructs an App directly without going through Wails, and would be
// nil there -- so nil is treated as "don't report progress" rather than
// ever risking that call.
func newScanProgressEmitter(ctx context.Context) func(done, total int) {
	if ctx == nil {
		return nil
	}
	return func(done, total int) {
		runtime.EventsEmit(ctx, "library:scan-progress", libraryScanProgress{Done: done, Total: total})
	}
}

func (a *App) ListLibrary(forceRefresh bool) (appapi.LibraryView, error) {
	return a.api.ListLibrary(forceRefresh, newScanProgressEmitter(a.ctx))
}

func (a *App) ListPDFCoverCandidates(bookPath string) ([]appapi.CoverCandidateView, error) {
	return a.api.ListPDFCoverCandidates(bookPath)
}

func (a *App) SetCoverOverride(bookPath string, page int) (string, error) {
	return a.api.SetCoverOverride(bookPath, page)
}

func (a *App) SetCoverOverrideCustomFromFile(bookPath, imagePath string) (string, error) {
	return a.api.SetCoverOverrideCustomFromFile(bookPath, imagePath)
}

func (a *App) ClearCoverOverride(bookPath string) (string, error) {
	return a.api.ClearCoverOverride(bookPath)
}

func (a *App) ResetCoverCache() error {
	return a.api.ResetCoverCache()
}

func (a *App) GetScanConcurrency() (appapi.ScanConcurrencyView, error) {
	return a.api.GetScanConcurrency()
}

func (a *App) SetScanConcurrency(n int) error {
	return a.api.SetScanConcurrency(n)
}

// PickCoverImageFile shows a native "choose an image" file dialog and
// returns the chosen path, or "" (not an error) if the user cancels --
// matching runtime.OpenFileDialog's own cancel behavior. The frontend
// passes the returned path straight to SetCoverOverrideCustomFromFile
// rather than reading the file itself, so image bytes never cross the
// Wails JS<->Go bridge as a base64 blob.
func (a *App) PickCoverImageFile() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose cover image",
		Filters: []runtime.FileFilter{
			{DisplayName: "Images (*.jpg, *.jpeg, *.png)", Pattern: "*.jpg;*.jpeg;*.png"},
		},
	})
}

// OpenFile opens the file at path in the OS default application for its
// type. This shells out to the platform's native opener rather than using
// runtime.BrowserOpenURL: Wails v2.13.0 added security hardening that
// rejects file:// URLs outright (blocking the file, javascript, data, and
// ftp schemes), so routing local files through it always fails with
// "Invalid URL scheme not allowed". The other real failure OpenFile can
// report is the file no longer existing at the recorded path -- a real
// scenario since Scan's results can go stale if the file is moved or
// deleted outside the app before Open is clicked.
func (a *App) OpenFile(path string) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	name, args := openCommand(path)
	if err := exec.Command(name, args...).Start(); err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	return nil
}

// openCommand returns the platform-native command used to open path in its
// default application.
func openCommand(path string) (string, []string) {
	switch osruntime.GOOS {
	case "darwin":
		return "open", []string{path}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", path}
	default:
		return "xdg-open", []string{path}
	}
}

// ConfirmApply shows a native Yes/No dialog before Apply runs, since moving
// files is a consequential action even though it can be undone from the
// Operations view. Returns true if the user confirmed.
func (a *App) ConfirmApply(fileCount int, libraryFolder string) bool {
	message := fmt.Sprintf(
		"Move %d file(s) into %s? You can undo this later from the Operations view.",
		fileCount, libraryFolder,
	)
	if libraryFolder == "" {
		message = fmt.Sprintf(
			"Move %d file(s) into the library folder? You can undo this later from the Operations view.",
			fileCount,
		)
	}
	result, err := runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:          runtime.QuestionDialog,
		Title:         "Apply changes?",
		Message:       message,
		Buttons:       []string{"Move files", "Cancel"},
		DefaultButton: "Cancel",
	})
	if err != nil {
		return false
	}
	return isAffirmative(result)
}

// ConfirmUndo shows a native Yes/No dialog before UndoBatch runs, mirroring
// ConfirmApply -- undoing moves files again, and without a Redo UI yet
// there's no quick way back from an accidental click.
func (a *App) ConfirmUndo(fileCount int) bool {
	message := fmt.Sprintf(
		"Move %d file(s) back to their original location?",
		fileCount,
	)
	result, err := runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:          runtime.QuestionDialog,
		Title:         "Undo this batch?",
		Message:       message,
		Buttons:       []string{"Undo", "Cancel"},
		DefaultButton: "Cancel",
	})
	if err != nil {
		return false
	}
	return isAffirmative(result)
}

// ConfirmResetCoverCache shows a native Yes/No dialog before
// ResetCoverCache runs, mirroring ConfirmApply/ConfirmUndo -- clearing
// hundreds of cached files and triggering a multi-minute rebuild on the
// next Library visit is disruptive enough to warrant a confirmation
// step, even though nothing genuinely irreplaceable is lost.
func (a *App) ConfirmResetCoverCache() bool {
	result, err := runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:  runtime.QuestionDialog,
		Title: "Reset cover cache?",
		Message: "This clears every cached cover image and the library scan " +
			"cache, so the next time you open or refresh the Library, every " +
			"book will be re-scanned from scratch. This does not affect any " +
			"covers you've manually chosen.",
		Buttons:       []string{"Reset", "Cancel"},
		DefaultButton: "Cancel",
	})
	if err != nil {
		return false
	}
	return isAffirmative(result)
}

// isAffirmative interprets a MessageDialog result. Wails only honors custom
// Buttons on macOS; on Linux/Windows the dialog falls back to a default
// Yes/No, so a literal check against the custom label alone silently
// rejects every confirmation on those platforms.
func isAffirmative(result string) bool {
	switch result {
	case "Move files", "Undo", "Reset", "Yes", "OK":
		return true
	default:
		return false
	}
}
