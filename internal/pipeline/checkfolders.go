package pipeline

import (
	"fmt"
	"os"

	"github.com/FrancisChung/book-organiser/internal/config"
)

// CheckFolders validates cfg.General's configured folders at initialization,
// before Run or any operations.Manager call -- catching a misconfigured or
// missing folder immediately, with a clear error, rather than partway
// through a scan or an apply batch. WorkingFolder must already exist as a
// directory: it's where books come from, so silently creating an empty
// inbox there would mask a misconfigured path and just produce a silent
// "0 books found" result. LibraryFolder and LogFolder are destinations, so
// they're created if missing (matching the auto-create behavior
// operations.MoveCommand and LogCategoryWarnings already apply to their own
// destination paths) -- CheckFolders just does it upfront instead of
// leaving it to whichever operation happens to need the folder first.
func CheckFolders(cfg config.Config) error {
	if cfg.General.WorkingFolder == "" {
		return fmt.Errorf("working_folder is not configured")
	}
	info, err := os.Stat(cfg.General.WorkingFolder)
	if err != nil {
		return fmt.Errorf("working_folder %q: %w", cfg.General.WorkingFolder, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("working_folder %q is not a directory", cfg.General.WorkingFolder)
	}

	if cfg.General.LibraryFolder == "" {
		return fmt.Errorf("library_folder is not configured")
	}
	if err := os.MkdirAll(cfg.General.LibraryFolder, 0755); err != nil {
		return fmt.Errorf("library_folder %q: %w", cfg.General.LibraryFolder, err)
	}

	if cfg.General.LogFolder == "" {
		return fmt.Errorf("log_folder is not configured")
	}
	if err := os.MkdirAll(cfg.General.LogFolder, 0755); err != nil {
		return fmt.Errorf("log_folder %q: %w", cfg.General.LogFolder, err)
	}

	return nil
}
