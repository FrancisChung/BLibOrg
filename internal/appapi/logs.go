package appapi

import (
	"path/filepath"
	"time"

	"github.com/FrancisChung/book-organiser/internal/pipeline"
)

// CategoryWarningView is the JSON-serializable view of a
// pipeline.CategoryWarningEntry sent to the frontend.
type CategoryWarningView struct {
	Timestamp   string `json:"timestamp"`
	SourcePath  string `json:"sourcePath"`
	Category    string `json:"category"`
	Subcategory string `json:"subcategory"`
	Warning     string `json:"warning"`
}

// ListCategoryWarnings returns every entry ever logged to
// log_folder/category-warnings.jsonl, newest first. An empty or
// never-written log returns an empty (non-nil) slice, not an error.
func (a *App) ListCategoryWarnings() ([]CategoryWarningView, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(cfg.General.LogFolder, "category-warnings.jsonl")
	entries, err := pipeline.ReadCategoryWarnings(path)
	if err != nil {
		return nil, err
	}

	views := make([]CategoryWarningView, 0, len(entries))
	for _, e := range entries {
		views = append(views, CategoryWarningView{
			Timestamp:   e.Timestamp.Format(time.RFC3339),
			SourcePath:  e.SourcePath,
			Category:    e.Category,
			Subcategory: e.Subcategory,
			Warning:     e.Warning,
		})
	}
	for i, j := 0, len(views)-1; i < j; i, j = i+1, j-1 {
		views[i], views[j] = views[j], views[i]
	}
	return views, nil
}
