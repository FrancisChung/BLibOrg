package appapi

import "github.com/FrancisChung/book-organiser/internal/pipeline"

// Scan loads the config, validates/creates the configured folders, runs
// the backend pipeline against the working folder, and returns every
// scanned book as a BookView. Category-warning logging failures are
// best-effort and don't block the scan result.
func (a *App) Scan() ([]BookView, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return nil, err
	}
	if err := pipeline.CheckFolders(cfg); err != nil {
		return nil, err
	}
	books, err := pipeline.Run(cfg)
	if err != nil {
		return nil, err
	}
	_ = pipeline.LogCategoryWarnings(books, cfg)

	views := make([]BookView, 0, len(books))
	for _, b := range books {
		views = append(views, bookToView(b))
	}
	return views, nil
}
