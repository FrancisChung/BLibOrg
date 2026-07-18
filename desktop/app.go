package main

import (
	"context"
	"fmt"
	"net/url"
	"os"

	"github.com/FrancisChung/book-organiser/internal/appapi"
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
	return a.api.Scan()
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

func (a *App) UndoBatch(batchID string) error {
	return a.api.UndoBatch(batchID)
}

// OpenFile opens the file at path in the OS default application for its
// type. runtime.BrowserOpenURL is fire-and-forget (it returns nothing), so
// the one failure this can actually report is the file no longer existing
// at the recorded path -- a real scenario since Scan's results can go
// stale if the file is moved or deleted outside the app before Open is
// clicked.
func (a *App) OpenFile(path string) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	runtime.BrowserOpenURL(a.ctx, fileURI(path))
	return nil
}

// fileURI converts an absolute filesystem path into a file:// URI, safely
// escaping characters (spaces, parentheses, etc.) that book filenames
// routinely contain but that aren't valid unescaped in a URI.
func fileURI(path string) string {
	u := url.URL{Scheme: "file", Path: path}
	return u.String()
}

// ConfirmApply shows a native Yes/No dialog before Apply runs, since
// moving files is the one hard-to-reverse action in this flow. Returns
// true if the user confirmed.
func (a *App) ConfirmApply(fileCount int, libraryFolder string) bool {
	message := fmt.Sprintf(
		"Move %d file(s) into %s? This can be undone later from the command line, but there is no in-app Undo yet.",
		fileCount, libraryFolder,
	)
	if libraryFolder == "" {
		message = fmt.Sprintf(
			"Move %d file(s) into the library folder? This can be undone later from the command line, but there is no in-app Undo yet.",
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

// isAffirmative interprets a MessageDialog result. Wails only honors custom
// Buttons on macOS; on Linux/Windows the dialog falls back to a default
// Yes/No, so a literal check against the custom label alone silently
// rejects every confirmation on those platforms.
func isAffirmative(result string) bool {
	switch result {
	case "Move files", "Undo", "Yes", "OK":
		return true
	default:
		return false
	}
}
