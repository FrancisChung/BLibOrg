package operations

import (
	"errors"
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

// TestMoveCommand_ExecuteRefusesToOverwriteExistingDestination reproduces
// the Finding-1 review scenario: os.Rename silently replaces an existing
// target file on Unix. If a file already exists at NewPath (e.g. the tool
// is re-run, or two books collide on the same computed path), Execute must
// refuse the move rather than destroy the pre-existing file -- the
// operations log only records OldPath/NewPath for the file being moved, so
// there would be no way to recover the clobbered file via Undo.
func TestMoveCommand_ExecuteRefusesToOverwriteExistingDestination(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.epub")
	newPath := filepath.Join(dir, "new.epub")
	if err := os.WriteFile(oldPath, []byte("source content"), 0644); err != nil {
		t.Fatalf("write source fixture: %v", err)
	}
	if err := os.WriteFile(newPath, []byte("pre-existing content"), 0644); err != nil {
		t.Fatalf("write destination fixture: %v", err)
	}

	cmd := NewMoveCommand("batch-1", oldPath, newPath)
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected Execute to return an error when destination already exists")
	}
	if !errors.Is(err, ErrDestinationExists) {
		t.Errorf("expected error to identify ErrDestinationExists via errors.Is, got: %v", err)
	}

	// The pre-existing destination file must be untouched...
	got, readErr := os.ReadFile(newPath)
	if readErr != nil {
		t.Fatalf("expected destination file to still exist: %v", readErr)
	}
	if string(got) != "pre-existing content" {
		t.Errorf("expected destination file content untouched, got %q", string(got))
	}
	// ...and the source file must not have moved at all.
	if _, statErr := os.Stat(oldPath); statErr != nil {
		t.Fatalf("expected source file to remain at oldPath (move must not happen at all): %v", statErr)
	}
}

// TestMoveCommand_UndoRefusesToOverwriteExistingDestination applies the same
// existence-check-and-refuse guard to Undo: if OldPath has since been
// reoccupied by something else, moving NewPath back onto it would clobber
// whatever is there now, just as in Execute.
func TestMoveCommand_UndoRefusesToOverwriteExistingDestination(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.epub")
	newPath := filepath.Join(dir, "new.epub")
	if err := os.WriteFile(newPath, []byte("moved content"), 0644); err != nil {
		t.Fatalf("write fixture at newPath: %v", err)
	}
	// Simulate oldPath having been reoccupied since the move happened.
	if err := os.WriteFile(oldPath, []byte("reoccupied content"), 0644); err != nil {
		t.Fatalf("write fixture at oldPath: %v", err)
	}

	cmd := NewMoveCommand("batch-1", oldPath, newPath)
	err := cmd.Undo()
	if err == nil {
		t.Fatal("expected Undo to return an error when oldPath is reoccupied")
	}
	if !errors.Is(err, ErrDestinationExists) {
		t.Errorf("expected error to identify ErrDestinationExists via errors.Is, got: %v", err)
	}

	got, readErr := os.ReadFile(oldPath)
	if readErr != nil {
		t.Fatalf("expected reoccupied file to still exist: %v", readErr)
	}
	if string(got) != "reoccupied content" {
		t.Errorf("expected reoccupied file content untouched, got %q", string(got))
	}
	if _, statErr := os.Stat(newPath); statErr != nil {
		t.Fatalf("expected file to remain at newPath (undo must not happen at all): %v", statErr)
	}
}

// TestMoveCommand_RedoRefusesToOverwriteExistingDestination confirms Redo
// (which delegates to Execute) inherits the same guard.
func TestMoveCommand_RedoRefusesToOverwriteExistingDestination(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.epub")
	newPath := filepath.Join(dir, "new.epub")
	if err := os.WriteFile(oldPath, []byte("source content"), 0644); err != nil {
		t.Fatalf("write source fixture: %v", err)
	}
	if err := os.WriteFile(newPath, []byte("pre-existing content"), 0644); err != nil {
		t.Fatalf("write destination fixture: %v", err)
	}

	cmd := NewMoveCommand("batch-1", oldPath, newPath)
	if err := cmd.Redo(); !errors.Is(err, ErrDestinationExists) {
		t.Errorf("expected Redo to refuse via ErrDestinationExists, got: %v", err)
	}
}

// TestMoveCommand_FallsBackToCopyThenDeleteOnRenameFailure exercises the
// Finding-3 cross-device fallback: EXDEV requires two real distinct
// filesystems, which a single t.TempDir() can't provide, so this test
// substitutes renameFunc (a package-level seam, mirroring manager.go's
// nowFunc pattern) with a fake that always fails, forcing Execute down the
// copy-then-delete path. This proves the fallback mechanics work; it does
// not prove real EXDEV detection, which is untestable without a genuine
// cross-device setup.
func TestMoveCommand_FallsBackToCopyThenDeleteOnRenameFailure(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.epub")
	newPath := filepath.Join(dir, "sub", "new.epub")
	if err := os.WriteFile(oldPath, []byte("cross-device content"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	original := renameFunc
	renameFunc = func(src, dst string) error {
		return errors.New("simulated EXDEV: invalid cross-device link")
	}
	t.Cleanup(func() { renameFunc = original })

	cmd := NewMoveCommand("batch-1", oldPath, newPath)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	got, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("expected file at newPath after fallback copy: %v", err)
	}
	if string(got) != "cross-device content" {
		t.Errorf("newPath content = %q, want %q", string(got), "cross-device content")
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("expected oldPath to be removed after fallback copy+delete")
	}
}

// TestCopyThenDelete_CopiesContentAndRemovesSource unit-tests the
// copy-then-delete helper directly, independent of whether a real EXDEV
// condition can be forced in this environment.
func TestCopyThenDelete_CopiesContentAndRemovesSource(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	src := filepath.Join(srcDir, "book.epub")
	dst := filepath.Join(dstDir, "book.epub")
	content := []byte("some book bytes")
	if err := os.WriteFile(src, content, 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if err := copyThenDelete(src, dst); err != nil {
		t.Fatalf("copyThenDelete error: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("expected file at dst: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("dst content = %q, want %q", string(got), string(content))
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("expected src to be removed after copyThenDelete")
	}
}

// TestCopyThenDelete_FailsWithoutTouchingSourceWhenDestinationCannotBeCreated
// confirms copyThenDelete fails cleanly (and leaves src untouched) when the
// destination can't be created at all -- here because dst is already
// occupied by a directory, so the O_EXCL create fails before any bytes are
// copied. This exercises the "no partial artifact left behind" guarantee
// for the create-failure branch. The mid-copy failure branches (io.Copy /
// Sync / Close error triggering an os.Remove(dst) cleanup) are simple,
// single-call cleanups that aren't independently forced here because
// portably simulating a write failure partway through a copy (e.g. via
// disk quota or a synthetic faulty reader) was judged not worth the added
// complexity for this fix -- see the task report for the full reasoning.
func TestCopyThenDelete_FailsWithoutTouchingSourceWhenDestinationCannotBeCreated(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	src := filepath.Join(srcDir, "book.epub")
	if err := os.WriteFile(src, []byte("content"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	dst := filepath.Join(dstDir, "book.epub")
	if err := os.Mkdir(dst, 0755); err != nil {
		t.Fatalf("create directory at dst: %v", err)
	}

	if err := copyThenDelete(src, dst); err == nil {
		t.Fatal("expected copyThenDelete to return an error when dst cannot be created")
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("expected src to remain untouched when copy never started: %v", err)
	}
}
