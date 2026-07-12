package operations

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// OpType identifies the kind of filesystem operation a Command performs.
type OpType string

const OpMove OpType = "move"

// ErrDestinationExists is returned (wrapped) by MoveCommand.Execute/Undo/
// Redo when the target path already has a file at it. Moves refuse to
// silently overwrite an existing file -- os.Rename on Unix would otherwise
// destroy whatever is at the destination with no way to recover it, since
// the operations log only records OldPath/NewPath for the file being
// moved, not for anything it clobbered along the way. Callers that want
// conflict resolution (auto-rename-with-suffix, prompting, etc.) must
// implement that themselves; this package only fails loudly.
var ErrDestinationExists = errors.New("destination already exists")

// renameFunc is a seam over os.Rename so tests can simulate a rename
// failure (e.g. a cross-device EXDEV-style error) without needing two
// genuinely distinct filesystems, mirroring the nowFunc pattern in
// manager.go.
var renameFunc = os.Rename

// CommandData is the serializable description of a Command: enough
// information to reconstruct it purely from a persisted log entry.
type CommandData struct {
	BatchID string
	OpType  OpType
	OldPath string
	NewPath string
}

// Command is a reversible unit of filesystem work. Undo must exactly
// reverse the effect of Execute, and Redo must be able to re-apply it.
type Command interface {
	Execute() error
	Undo() error
	Redo() error
	Data() CommandData
}

// MoveCommand moves a file from OldPath to NewPath, creating any missing
// destination directories. Undo moves it back. Execute, Undo, and Redo all
// refuse to overwrite an existing file at their target path (see
// ErrDestinationExists) rather than silently clobbering it.
//
// Cross-device moves: os.Rename fails when the source and destination are
// on different filesystems/devices (EXDEV on Unix, effectively
// ERROR_NOT_SAME_DEVICE on Windows) -- a realistic scenario for this tool
// (e.g. inbox on one drive, library on a NAS or a different volume). When
// the underlying rename fails, MoveCommand falls back to copying the
// file's contents to the destination and then removing the source, so
// cross-device moves succeed instead of erroring out. Because Go's stdlib
// does not expose a fully portable, cross-OS-checkable EXDEV sentinel, the
// fallback is attempted on ANY rename failure rather than being restricted
// to a specific error code: a failed rename whose fallback also fails just
// surfaces the more informative copy+delete error anyway, so this is not a
// correctness gap.
type MoveCommand struct {
	data CommandData
}

func NewMoveCommand(batchID, oldPath, newPath string) *MoveCommand {
	return &MoveCommand{data: CommandData{BatchID: batchID, OpType: OpMove, OldPath: oldPath, NewPath: newPath}}
}

func (c *MoveCommand) Execute() error {
	return move(c.data.OldPath, c.data.NewPath)
}

func (c *MoveCommand) Undo() error {
	return move(c.data.NewPath, c.data.OldPath)
}

func (c *MoveCommand) Redo() error {
	return c.Execute()
}

func (c *MoveCommand) Data() CommandData {
	return c.data
}

// move relocates the file at src to dst. It refuses to overwrite an
// existing file at dst (ErrDestinationExists), creates any missing
// destination directories, and falls back to copyThenDelete if a direct
// rename fails -- see the MoveCommand doc comment for why.
func move(src, dst string) error {
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("move %s to %s: %w", src, dst, ErrDestinationExists)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("move %s to %s: check destination: %w", src, dst, err)
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("create destination dir for %s: %w", dst, err)
	}

	if renameErr := renameFunc(src, dst); renameErr != nil {
		if copyErr := copyThenDelete(src, dst); copyErr != nil {
			return fmt.Errorf("move %s to %s: rename failed (%v) and copy+delete fallback also failed: %w", src, dst, renameErr, copyErr)
		}
	}
	return nil
}

// copyThenDelete copies src's contents to dst, syncs and closes dst, then
// removes src. It is the fallback used when a direct rename cannot work
// (typically because src and dst are on different filesystems/devices). If
// the copy fails partway through, the partially-written destination file
// is removed so a retry doesn't get confused by a half-written file
// already sitting at dst.
func copyThenDelete(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source %s: %w", src, err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return fmt.Errorf("create destination %s: %w", dst, err)
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return fmt.Errorf("copy %s to %s: %w", src, dst, err)
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(dst)
		return fmt.Errorf("sync %s: %w", dst, err)
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return fmt.Errorf("close %s: %w", dst, err)
	}
	if err := os.Remove(src); err != nil {
		return fmt.Errorf("remove source %s after copy: %w", src, err)
	}
	return nil
}
