package main

import (
	"context"
	"fmt"

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
	return result == "Move files"
}
