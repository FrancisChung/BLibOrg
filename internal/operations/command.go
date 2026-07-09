package operations

import (
	"fmt"
	"os"
	"path/filepath"
)

// OpType identifies the kind of filesystem operation a Command performs.
type OpType string

const OpMove OpType = "move"

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
// destination directories. Undo moves it back.
type MoveCommand struct {
	data CommandData
}

func NewMoveCommand(batchID, oldPath, newPath string) *MoveCommand {
	return &MoveCommand{data: CommandData{BatchID: batchID, OpType: OpMove, OldPath: oldPath, NewPath: newPath}}
}

func (c *MoveCommand) Execute() error {
	if err := os.MkdirAll(filepath.Dir(c.data.NewPath), 0755); err != nil {
		return fmt.Errorf("create destination dir for %s: %w", c.data.NewPath, err)
	}
	if err := os.Rename(c.data.OldPath, c.data.NewPath); err != nil {
		return fmt.Errorf("move %s to %s: %w", c.data.OldPath, c.data.NewPath, err)
	}
	return nil
}

func (c *MoveCommand) Undo() error {
	if err := os.MkdirAll(filepath.Dir(c.data.OldPath), 0755); err != nil {
		return fmt.Errorf("create source dir for %s: %w", c.data.OldPath, err)
	}
	if err := os.Rename(c.data.NewPath, c.data.OldPath); err != nil {
		return fmt.Errorf("move %s back to %s: %w", c.data.NewPath, c.data.OldPath, err)
	}
	return nil
}

func (c *MoveCommand) Redo() error {
	return c.Execute()
}

func (c *MoveCommand) Data() CommandData {
	return c.data
}
