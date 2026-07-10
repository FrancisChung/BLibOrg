package operations

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManager_ExecuteBatchThenUndoThenRedo(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.epub")
	newPath := filepath.Join(dir, "Fiction", "new.epub")
	if err := os.WriteFile(oldPath, []byte("content"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	logPath := filepath.Join(dir, "ops.jsonl")
	mgr := NewManager(NewLog(logPath))
	cmd := NewMoveCommand("batch-1", oldPath, newPath)

	if err := mgr.ExecuteBatch("batch-1", []Command{cmd}); err != nil {
		t.Fatalf("ExecuteBatch error: %v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("expected file at newPath: %v", err)
	}

	// Simulate an app restart: build a brand new Manager pointed at the same
	// log file, with no in-memory state carried over.
	restarted := NewManager(NewLog(logPath))
	if err := restarted.UndoBatch("batch-1"); err != nil {
		t.Fatalf("UndoBatch error: %v", err)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("expected file restored to oldPath: %v", err)
	}

	if err := restarted.RedoBatch("batch-1"); err != nil {
		t.Fatalf("RedoBatch error: %v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("expected file moved to newPath again: %v", err)
	}
}

func TestManager_ExecuteBatchRollsBackOnPartialFailure(t *testing.T) {
	dir := t.TempDir()
	oldPath1 := filepath.Join(dir, "old1.epub")
	newPath1 := filepath.Join(dir, "new1.epub")
	if err := os.WriteFile(oldPath1, []byte("content"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	// oldPath2 deliberately does not exist, so its move will fail.
	oldPath2 := filepath.Join(dir, "does-not-exist.epub")
	newPath2 := filepath.Join(dir, "new2.epub")

	logPath := filepath.Join(dir, "ops.jsonl")
	mgr := NewManager(NewLog(logPath))
	cmds := []Command{
		NewMoveCommand("batch-2", oldPath1, newPath1),
		NewMoveCommand("batch-2", oldPath2, newPath2),
	}

	err := mgr.ExecuteBatch("batch-2", cmds)
	if err == nil {
		t.Fatal("expected ExecuteBatch to return an error when a command fails")
	}

	if _, err := os.Stat(oldPath1); err != nil {
		t.Errorf("expected first move to be rolled back (file back at oldPath1): %v", err)
	}
	if _, err := os.Stat(newPath1); !os.IsNotExist(err) {
		t.Errorf("expected newPath1 to not exist after rollback")
	}

	entries, err := NewLog(logPath).ReadAll()
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no log entries for a failed batch, got %d", len(entries))
	}
}

// TestManager_ExecuteBatchRollsBackFilesIfLogAppendFails reproduces a real
// incident: all commands in a batch executed successfully (files moved), but
// persisting the log entry then failed (here: because the log's directory
// couldn't be created), leaving the files moved with zero record in the
// log -- meaning UndoBatch has nothing to undo. ExecuteBatch must treat a
// log-append failure exactly like a command-execution failure: roll back
// every already-executed command before returning, so the library is never
// left moved-but-unlogged regardless of why the log write failed.
func TestManager_ExecuteBatchRollsBackFilesIfLogAppendFails(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.epub")
	newPath := filepath.Join(dir, "new.epub")
	if err := os.WriteFile(oldPath, []byte("content"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	// A regular file sits where the log's parent directory needs to be, so
	// os.MkdirAll (and thus Append) can never succeed here, regardless of
	// the missing-directory fix -- this exercises the log-append-failure
	// path for any underlying cause, not just a missing directory.
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	logPath := filepath.Join(blocker, "ops.jsonl")

	mgr := NewManager(NewLog(logPath))
	cmd := NewMoveCommand("batch-7", oldPath, newPath)

	err := mgr.ExecuteBatch("batch-7", []Command{cmd})
	if err == nil {
		t.Fatal("expected ExecuteBatch to return an error when the log append fails")
	}
	if _, statErr := os.Stat(oldPath); statErr != nil {
		t.Errorf("expected the move to be rolled back (file back at oldPath) after a log-append failure: %v", statErr)
	}
	if _, statErr := os.Stat(newPath); !os.IsNotExist(statErr) {
		t.Errorf("expected newPath to not exist after rollback")
	}
}

func TestManager_UndoBatchUnknownIDIsNoOp(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "ops.jsonl")
	mgr := NewManager(NewLog(logPath))

	if err := mgr.UndoBatch("no-such-batch"); err != nil {
		t.Fatalf("UndoBatch on unknown batch should be a no-op, got error: %v", err)
	}
}

func TestManager_UndoBatchTwiceIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.epub")
	newPath := filepath.Join(dir, "new.epub")
	if err := os.WriteFile(oldPath, []byte("content"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	logPath := filepath.Join(dir, "ops.jsonl")
	mgr := NewManager(NewLog(logPath))
	cmd := NewMoveCommand("batch-3", oldPath, newPath)
	if err := mgr.ExecuteBatch("batch-3", []Command{cmd}); err != nil {
		t.Fatalf("ExecuteBatch error: %v", err)
	}

	if err := mgr.UndoBatch("batch-3"); err != nil {
		t.Fatalf("first UndoBatch error: %v", err)
	}
	// Second undo should be a no-op: the entry is already marked Undone, so
	// there is nothing left to reverse and it must not attempt to move a
	// file that no longer exists at newPath.
	if err := mgr.UndoBatch("batch-3"); err != nil {
		t.Fatalf("second UndoBatch should be a no-op, got error: %v", err)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("expected file to remain at oldPath: %v", err)
	}
}

func TestManager_RedoBatchTwiceIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.epub")
	newPath := filepath.Join(dir, "new.epub")
	if err := os.WriteFile(oldPath, []byte("content"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	logPath := filepath.Join(dir, "ops.jsonl")
	mgr := NewManager(NewLog(logPath))
	cmd := NewMoveCommand("batch-4", oldPath, newPath)
	if err := mgr.ExecuteBatch("batch-4", []Command{cmd}); err != nil {
		t.Fatalf("ExecuteBatch error: %v", err)
	}
	if err := mgr.UndoBatch("batch-4"); err != nil {
		t.Fatalf("UndoBatch error: %v", err)
	}

	if err := mgr.RedoBatch("batch-4"); err != nil {
		t.Fatalf("first RedoBatch error: %v", err)
	}
	// Second redo should be a no-op: nothing is marked Undone anymore.
	if err := mgr.RedoBatch("batch-4"); err != nil {
		t.Fatalf("second RedoBatch should be a no-op, got error: %v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("expected file to remain at newPath: %v", err)
	}
}

// TestManager_UndoBatchRetryAfterPartialFailureRecovers reproduces the
// "permanently stuck" scenario from the review finding: a batch of three
// moves is undone most-recent-first; the middle entry's reversal fails
// partway through. Without incrementally persisting progress, the entry
// that *did* get reversed before the failure (the most recent one) is never
// marked Undone in the log, so a retry re-selects it, tries to move a file
// that is no longer at NewPath, and fails again forever. With incremental
// persistence, the retry should only re-attempt entries that still
// genuinely need reversing, and should succeed once the underlying cause is
// fixed.
func TestManager_UndoBatchRetryAfterPartialFailureRecovers(t *testing.T) {
	dir := t.TempDir()
	oldPath1 := filepath.Join(dir, "old1.epub")
	newPath1 := filepath.Join(dir, "new1.epub")
	oldPath2 := filepath.Join(dir, "old2.epub")
	newPath2 := filepath.Join(dir, "new2.epub")
	oldPath3 := filepath.Join(dir, "old3.epub")
	newPath3 := filepath.Join(dir, "new3.epub")
	for _, p := range []string{oldPath1, oldPath2, oldPath3} {
		if err := os.WriteFile(p, []byte("content"), 0644); err != nil {
			t.Fatalf("write fixture %s: %v", p, err)
		}
	}

	logPath := filepath.Join(dir, "ops.jsonl")
	mgr := NewManager(NewLog(logPath))
	cmds := []Command{
		NewMoveCommand("batch-5", oldPath1, newPath1),
		NewMoveCommand("batch-5", oldPath2, newPath2),
		NewMoveCommand("batch-5", oldPath3, newPath3),
	}
	if err := mgr.ExecuteBatch("batch-5", cmds); err != nil {
		t.Fatalf("ExecuteBatch error: %v", err)
	}

	// UndoBatch reverses most-recent-first, so the attempt order is
	// entry3, entry2, entry1. Sabotage entry2's reversal by removing the
	// file UndoBatch expects to move back, so its Undo() fails partway
	// through the batch, after entry3 has already been successfully
	// reversed.
	if err := os.Remove(newPath2); err != nil {
		t.Fatalf("sabotage newPath2: %v", err)
	}

	err := mgr.UndoBatch("batch-5")
	if err == nil {
		t.Fatal("expected first UndoBatch call to fail on the sabotaged entry")
	}
	if _, statErr := os.Stat(oldPath3); statErr != nil {
		t.Fatalf("expected entry3 to have been reversed before the failure: %v", statErr)
	}
	if _, statErr := os.Stat(newPath1); statErr != nil {
		t.Fatalf("expected entry1 to NOT have been attempted yet (still at newPath1): %v", statErr)
	}

	// Fix the cause: restore the file UndoBatch was trying to move back.
	if err := os.WriteFile(newPath2, []byte("content"), 0644); err != nil {
		t.Fatalf("un-sabotage newPath2: %v", err)
	}

	// Retry. If entry3's already-successful reversal wasn't persisted, this
	// retry re-selects it and fails immediately because newPath3 no longer
	// exists -- the batch would be permanently stuck.
	if err := mgr.UndoBatch("batch-5"); err != nil {
		t.Fatalf("retry UndoBatch should succeed once the cause is fixed, got error: %v", err)
	}

	for _, p := range []string{oldPath1, oldPath2, oldPath3} {
		if _, statErr := os.Stat(p); statErr != nil {
			t.Errorf("expected file restored to %s: %v", p, statErr)
		}
	}
	for _, p := range []string{newPath1, newPath2, newPath3} {
		if _, statErr := os.Stat(p); !os.IsNotExist(statErr) {
			t.Errorf("expected %s to no longer exist after full undo", p)
		}
	}
}

// fakeCommand is a trivial in-test Command implementation that returns
// canned errors, used to exercise Manager's error-folding logic without
// needing real file contrivance.
type fakeCommand struct {
	data    CommandData
	execErr error
	undoErr error
	redoErr error
}

func (f *fakeCommand) Execute() error    { return f.execErr }
func (f *fakeCommand) Undo() error       { return f.undoErr }
func (f *fakeCommand) Redo() error       { return f.redoErr }
func (f *fakeCommand) Data() CommandData { return f.data }

// TestManager_ExecuteBatchSurfacesRollbackFailureAsAuxiliaryContext exercises
// the path in ExecuteBatch's rollback helper where undoing an
// already-executed command itself fails. The contract is that the rollback
// failure must not replace the primary cause -- it is folded in as
// auxiliary context so the caller still learns why the batch failed in the
// first place, while also being told the library may be left partially
// moved.
func TestManager_ExecuteBatchSurfacesRollbackFailureAsAuxiliaryContext(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "ops.jsonl")
	mgr := NewManager(NewLog(logPath))

	primaryErr := errors.New("execute boom")
	rollbackErr := errors.New("undo boom")

	cmd1 := &fakeCommand{
		data:    CommandData{BatchID: "batch-6", OpType: OpMove, OldPath: "/a", NewPath: "/b"},
		undoErr: rollbackErr,
	}
	cmd2 := &fakeCommand{
		data:    CommandData{BatchID: "batch-6", OpType: OpMove, OldPath: "/c", NewPath: "/d"},
		execErr: primaryErr,
	}

	err := mgr.ExecuteBatch("batch-6", []Command{cmd1, cmd2})
	if err == nil {
		t.Fatal("expected ExecuteBatch to return an error")
	}
	if !errors.Is(err, primaryErr) {
		t.Errorf("expected returned error to identify the primary execute failure via errors.Is, got: %v", err)
	}
	if !strings.Contains(err.Error(), rollbackErr.Error()) {
		t.Errorf("expected returned error to surface the rollback failure as context, got: %v", err)
	}
}
