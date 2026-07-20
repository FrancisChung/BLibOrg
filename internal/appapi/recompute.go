package appapi

import (
	"github.com/FrancisChung/book-organiser/internal/book"
	"github.com/FrancisChung/book-organiser/internal/categorizer"
	"github.com/FrancisChung/book-organiser/internal/rename"
	"github.com/FrancisChung/book-organiser/internal/textutil"
)

// Recompute takes a book with its Title/Author/Year fields as edited by
// the user (the frontend is responsible for marking whichever field it
// just changed as Source "Edited" before calling this), re-runs
// categorization and destination-path building server-side, and returns
// the updated view. It never touches the filesystem.
//
// A manually-edited Title (Source "Edited") is also passed through
// textutil.FormatTitle before categorization/path-building, so the same
// separator/casing cleanup applied to metadata-extracted titles at scan
// time also applies when the user types one in by hand. A Metadata- or
// Heuristic-sourced Title is left as-is here -- Recompute doesn't re-run
// extraction, so that formatting already happened (or didn't apply) at
// scan time.
func (a *App) Recompute(edited BookView) (BookView, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return BookView{}, err
	}
	b := viewToBook(edited)
	if b.Title.Source == book.SourceManual {
		b.Title.Value = textutil.FormatTitle(b.Title.Value, cfg.TitleFormatting.HyphenExceptions)
	}
	categorizer.Categorize(b, cfg)
	rename.BuildPath(b, cfg)
	return bookToView(b), nil
}
