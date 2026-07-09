package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/FrancisChung/book-organiser/internal/book"
	"github.com/FrancisChung/book-organiser/internal/config"
)

var nowFunc = time.Now

// CategoryWarningEntry is one persisted record of a book.Book.CategoryWarning
// -- a rule that matched a category/subcategory not declared in cfg.Categories.
type CategoryWarningEntry struct {
	Timestamp   time.Time `json:"timestamp"`
	SourcePath  string    `json:"source_path"`
	Category    string    `json:"category"`
	Subcategory string    `json:"subcategory"`
	Warning     string    `json:"warning"`
}

// LogCategoryWarnings appends one JSONL entry per book carrying a non-empty
// CategoryWarning to cfg.General.LogFolder/category-warnings.jsonl, creating
// the log folder if needed. pipeline.Run itself stays read-only with no file
// side effects; callers invoke this explicitly after Run so a future UI layer
// can read the log and surface these warnings to the user. A no-op (no file
// created) when no book in the batch carries a warning.
func LogCategoryWarnings(books []*book.Book, cfg config.Config) error {
	var toLog []*book.Book
	for _, b := range books {
		if b.CategoryWarning != "" {
			toLog = append(toLog, b)
		}
	}
	if len(toLog) == 0 {
		return nil
	}

	if err := os.MkdirAll(cfg.General.LogFolder, 0755); err != nil {
		return err
	}
	path := filepath.Join(cfg.General.LogFolder, "category-warnings.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, b := range toLog {
		entry := CategoryWarningEntry{
			Timestamp:   nowFunc(),
			SourcePath:  b.SourcePath,
			Category:    b.Category,
			Subcategory: b.Subcategory,
			Warning:     b.CategoryWarning,
		}
		if err := enc.Encode(entry); err != nil {
			return err
		}
	}
	return nil
}
