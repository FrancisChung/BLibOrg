package appapi

import (
	"path/filepath"

	"github.com/FrancisChung/BLibOrg/internal/book"
	"github.com/FrancisChung/BLibOrg/internal/operations"
)

type ApplyResultEntry struct {
	SourcePath string `json:"sourcePath"`
	OK         bool   `json:"ok"`
	Error      string `json:"error"`
	Skipped    bool   `json:"skipped"`
}

type ApplyResult struct {
	BatchID string             `json:"batchId"`
	Results []ApplyResultEntry `json:"results"`
}

// Apply moves every non-Unresolved book to its (already-computed, as last
// returned by Scan/Recompute) DestPath via the existing operation log, so
// the move can be undone from the Operations view.
// ExecuteBatch is all-or-nothing (it rolls back the whole batch on any
// failure), so on error every attempted row is reported as failed with the
// same error message -- there is no partial-success case within one Apply
// call.
func (a *App) Apply(books []BookView) (ApplyResult, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return ApplyResult{}, err
	}

	opsLog := operations.NewLog(filepath.Join(cfg.General.LogFolder, "ops.jsonl"))
	batchID, err := operations.NextBatchID(opsLog)
	if err != nil {
		return ApplyResult{}, err
	}

	var cmds []operations.Command
	var applying []BookView
	results := make([]ApplyResultEntry, 0, len(books))
	for _, v := range books {
		b := viewToBook(v)
		if b.Status() == book.SourceUnresolved {
			results = append(results, ApplyResultEntry{SourcePath: v.SourcePath, Skipped: true})
			continue
		}
		cmds = append(cmds, operations.NewMoveCommand(batchID, v.SourcePath, v.DestPath))
		applying = append(applying, v)
	}
	if len(cmds) == 0 {
		return ApplyResult{BatchID: batchID, Results: results}, nil
	}

	mgr := operations.NewManager(opsLog)
	if err := mgr.ExecuteBatch(batchID, cmds); err != nil {
		for _, v := range applying {
			results = append(results, ApplyResultEntry{SourcePath: v.SourcePath, Error: err.Error()})
		}
		return ApplyResult{BatchID: batchID, Results: results}, nil
	}
	for _, v := range applying {
		results = append(results, ApplyResultEntry{SourcePath: v.SourcePath, OK: true})
	}
	return ApplyResult{BatchID: batchID, Results: results}, nil
}
