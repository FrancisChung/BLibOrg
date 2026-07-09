package operations

import (
	"os"
	"path/filepath"
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
