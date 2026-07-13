package pipeline

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/FrancisChung/book-organiser/internal/book"
	"github.com/FrancisChung/book-organiser/internal/config"
)

// ErrLogFolderNotConfigured is returned by LogCategoryWarnings when there are
// warnings to persist but cfg.General.LogFolder is unset -- config.Load
// doesn't default this field, so an existing or hand-edited config that
// omits log_folder is a realistic case, not a hypothetical one.
var ErrLogFolderNotConfigured = errors.New("general.log_folder is not configured")

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
	if cfg.General.LogFolder == "" {
		return ErrLogFolderNotConfigured
	}

	if err := os.MkdirAll(cfg.General.LogFolder, 0755); err != nil {
		return fmt.Errorf("create log folder %s: %w", cfg.General.LogFolder, err)
	}
	path := filepath.Join(cfg.General.LogFolder, "category-warnings.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open log %s for append: %w", path, err)
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
			return fmt.Errorf("write log entry for %s: %w", b.SourcePath, err)
		}
	}
	return nil
}

// ReadCategoryWarnings reads every entry from a category-warnings log file
// written by LogCategoryWarnings, in file order (oldest first). A missing
// file is treated as an empty log, not an error -- the log may not exist
// yet if no scan has ever produced a warning, matching operations.Log's
// existing ReadAll behavior for the same not-written-yet case.
func ReadCategoryWarnings(path string) ([]CategoryWarningEntry, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open log %s: %w", path, err)
	}
	defer f.Close()

	var entries []CategoryWarningEntry
	dec := json.NewDecoder(f)
	for dec.More() {
		var e CategoryWarningEntry
		if err := dec.Decode(&e); err != nil {
			return nil, fmt.Errorf("parse log entry: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, nil
}
