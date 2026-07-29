package appapi

import (
	"path/filepath"

	"github.com/FrancisChung/BLibOrg/internal/operations"
)

// UndoBatch reverses every not-yet-undone entry in batchID via the existing
// operation log, restoring each file to its original location. It is a
// no-op returning nil for an unknown batch ID or one that's already fully
// undone -- see operations.Manager.UndoBatch for the underlying semantics,
// including why it stops at the first failing entry rather than continuing
// through the rest (each entry's Undone flag is persisted as it succeeds,
// so a retry only re-attempts what's left).
func (a *App) UndoBatch(batchID string) error {
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	opsLog := operations.NewLog(filepath.Join(cfg.General.LogFolder, "ops.jsonl"))
	mgr := operations.NewManager(opsLog)
	return mgr.UndoBatch(batchID)
}
