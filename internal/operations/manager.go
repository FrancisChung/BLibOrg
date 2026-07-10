package operations

import (
	"errors"
	"fmt"
	"time"
)

var nowFunc = time.Now

// Manager executes, undoes, and redoes batches of Commands, persisting every
// successful batch to a Log so undo/redo survives an app restart.
type Manager struct {
	log *Log
}

func NewManager(log *Log) *Manager {
	return &Manager{log: log}
}

// ExecuteBatch runs commands in order. If a command fails partway through,
// or if persisting the batch to the log afterward fails for any reason,
// already-executed commands in this batch are undone (best-effort) before
// returning the error -- so the library is never left moved-but-unlogged,
// which would otherwise leave UndoBatch with nothing to undo.
//
// Rollback is best-effort: if undoing an already-executed command also
// fails (e.g. the destination was concurrently modified), that failure is
// not swallowed -- it is folded into the returned error so the caller knows
// the library may be left in a partially-moved state requiring manual
// intervention, rather than silently believing the rollback succeeded.
func (m *Manager) ExecuteBatch(batchID string, commands []Command) error {
	var executed []Command
	for _, cmd := range commands {
		if err := cmd.Execute(); err != nil {
			execErr := fmt.Errorf("batch %s failed on %+v: %w", batchID, cmd.Data(), err)
			return m.foldRollback(executed, execErr)
		}
		executed = append(executed, cmd)
	}

	entries := make([]LogEntry, len(commands))
	for i, cmd := range commands {
		d := cmd.Data()
		entries[i] = LogEntry{
			BatchID:   batchID,
			Timestamp: nowFunc(),
			OpType:    d.OpType,
			OldPath:   d.OldPath,
			NewPath:   d.NewPath,
			Undone:    false,
		}
	}
	if err := m.log.Append(entries); err != nil {
		recordErr := fmt.Errorf("record batch %s: %w", batchID, err)
		return m.foldRollback(executed, recordErr)
	}
	return nil
}

// foldRollback undoes executed (best-effort) and folds any rollback failure
// into primaryErr as auxiliary context, never replacing it.
func (m *Manager) foldRollback(executed []Command, primaryErr error) error {
	rollbackErr := m.rollback(executed)
	if rollbackErr != nil {
		return fmt.Errorf("%w (additionally, rollback failed: %v)", primaryErr, rollbackErr)
	}
	return primaryErr
}

// rollback undoes commands in reverse order, collecting (rather than
// discarding) any errors so the caller can be told rollback was incomplete.
func (m *Manager) rollback(executed []Command) error {
	var errs []error
	for i := len(executed) - 1; i >= 0; i-- {
		if err := executed[i].Undo(); err != nil {
			errs = append(errs, fmt.Errorf("undo %+v: %w", executed[i].Data(), err))
		}
	}
	return errors.Join(errs...)
}

// UndoBatch reverses every not-yet-undone entry for batchID, most recent
// first, by reconstructing commands purely from the persisted log -- this is
// what makes undo survive an app restart. Calling it for a batchID that
// doesn't exist in the log, or one whose entries are already all undone, is
// a no-op that returns nil.
//
// Each entry's Undone flag is persisted immediately after that entry is
// individually reversed, rather than only once the whole batch succeeds. If
// an entry fails partway through, the entries reversed before it are
// already correctly marked Undone in the log and the entries from it
// onward are correctly left alone, so a retried UndoBatch call re-selects
// only the entries that genuinely still need reversing instead of getting
// permanently stuck retrying entries whose file has already moved.
func (m *Manager) UndoBatch(batchID string) error {
	entries, err := m.log.ReadAll()
	if err != nil {
		return err
	}
	var toUndo []LogEntry
	for _, e := range entries {
		if e.BatchID == batchID && !e.Undone {
			toUndo = append(toUndo, e)
		}
	}
	if len(toUndo) == 0 {
		return nil
	}
	for i := len(toUndo) - 1; i >= 0; i-- {
		e := toUndo[i]
		cmd := NewMoveCommand(e.BatchID, e.OldPath, e.NewPath)
		if err := cmd.Undo(); err != nil {
			return fmt.Errorf("undo batch %s failed on %s: %w", batchID, e.NewPath, err)
		}
		if err := m.log.SetEntryUndone(e.BatchID, e.OldPath, e.NewPath, true); err != nil {
			return fmt.Errorf("undo batch %s: persist progress for %s: %w", batchID, e.NewPath, err)
		}
	}
	return nil
}

// RedoBatch re-applies every undone entry for batchID, in original order.
// Calling it for a batchID that doesn't exist in the log, or one whose
// entries are not currently undone, is a no-op that returns nil.
//
// As with UndoBatch, each entry's Undone flag is persisted immediately
// after that entry is individually re-applied, so a partial failure leaves
// the log accurate and a retry only re-attempts entries that still
// genuinely need redoing.
func (m *Manager) RedoBatch(batchID string) error {
	entries, err := m.log.ReadAll()
	if err != nil {
		return err
	}
	var toRedo []LogEntry
	for _, e := range entries {
		if e.BatchID == batchID && e.Undone {
			toRedo = append(toRedo, e)
		}
	}
	if len(toRedo) == 0 {
		return nil
	}
	for _, e := range toRedo {
		cmd := NewMoveCommand(e.BatchID, e.OldPath, e.NewPath)
		if err := cmd.Redo(); err != nil {
			return fmt.Errorf("redo batch %s failed on %s: %w", batchID, e.OldPath, err)
		}
		if err := m.log.SetEntryUndone(e.BatchID, e.OldPath, e.NewPath, false); err != nil {
			return fmt.Errorf("redo batch %s: persist progress for %s: %w", batchID, e.OldPath, err)
		}
	}
	return nil
}
