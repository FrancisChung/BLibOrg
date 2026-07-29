package appapi

import (
	"path/filepath"
	"time"

	"github.com/FrancisChung/BLibOrg/internal/operations"
	"github.com/FrancisChung/BLibOrg/internal/pipeline"
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

// OperationEntryView is the JSON-serializable view of one move operation
// within a batch.
type OperationEntryView struct {
	OldPath string `json:"oldPath"`
	NewPath string `json:"newPath"`
	OpType  string `json:"opType"`
	Undone  bool   `json:"undone"`
}

// OperationBatchView groups every operations.LogEntry with the same
// BatchID into one row for the UI -- a batch of 16 moved files should read
// as one entry, not 16 near-identical rows.
type OperationBatchView struct {
	BatchID     string               `json:"batchId"`
	Timestamp   string               `json:"timestamp"`
	EntryCount  int                  `json:"entryCount"`
	UndoneCount int                  `json:"undoneCount"`
	Entries     []OperationEntryView `json:"entries"`
}

// ListOperationBatches returns every batch ever recorded to
// log_folder/ops.jsonl, newest first, each with its individual file
// entries attached. An empty or never-written log returns an empty
// (non-nil) slice, not an error.
func (a *App) ListOperationBatches() ([]OperationBatchView, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return nil, err
	}
	opsLog := operations.NewLog(filepath.Join(cfg.General.LogFolder, "ops.jsonl"))

	summaries, err := opsLog.ListBatches()
	if err != nil {
		return nil, err
	}
	entries, err := opsLog.ReadAll()
	if err != nil {
		return nil, err
	}

	entriesByBatch := map[string][]operations.LogEntry{}
	for _, e := range entries {
		entriesByBatch[e.BatchID] = append(entriesByBatch[e.BatchID], e)
	}

	views := make([]OperationBatchView, 0, len(summaries))
	for _, s := range summaries {
		view := OperationBatchView{
			BatchID:     s.BatchID,
			Timestamp:   s.Timestamp.Format(time.RFC3339),
			EntryCount:  s.EntryCount,
			UndoneCount: s.UndoneCount,
		}
		for _, e := range entriesByBatch[s.BatchID] {
			view.Entries = append(view.Entries, OperationEntryView{
				OldPath: e.OldPath,
				NewPath: e.NewPath,
				OpType:  string(e.OpType),
				Undone:  e.Undone,
			})
		}
		views = append(views, view)
	}

	// ListBatches returns chronological (oldest-first) order; reverse for newest-first.
	for i, j := 0, len(views)-1; i < j; i, j = i+1, j-1 {
		views[i], views[j] = views[j], views[i]
	}
	return views, nil
}
