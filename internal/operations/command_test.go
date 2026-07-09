package operations

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMoveCommand_ExecuteUndoRedo(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.epub")
	newPath := filepath.Join(dir, "sub", "new.epub")
	if err := os.WriteFile(oldPath, []byte("content"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	cmd := NewMoveCommand("batch-1", oldPath, newPath)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("expected file at newPath after Execute: %v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("expected oldPath to be gone after Execute")
	}

	if err := cmd.Undo(); err != nil {
		t.Fatalf("Undo() error: %v", err)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("expected file back at oldPath after Undo: %v", err)
	}

	if err := cmd.Redo(); err != nil {
		t.Fatalf("Redo() error: %v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("expected file at newPath after Redo: %v", err)
	}
}
