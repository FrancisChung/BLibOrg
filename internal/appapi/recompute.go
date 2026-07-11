package appapi

import (
	"github.com/FrancisChung/book-organiser/internal/categorizer"
	"github.com/FrancisChung/book-organiser/internal/rename"
)

// Recompute takes a book with its Title/Author/Year fields as edited by
// the user (the frontend is responsible for marking whichever field it
// just changed as Source "Edited" before calling this), re-runs
// categorization and destination-path building server-side, and returns
// the updated view. It never touches the filesystem.
func (a *App) Recompute(edited BookView) (BookView, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return BookView{}, err
	}
	b := viewToBook(edited)
	categorizer.Categorize(b, cfg)
	rename.BuildPath(b, cfg)
	return bookToView(b), nil
}
