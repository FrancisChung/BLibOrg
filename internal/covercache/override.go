// This file persists manual cover overrides -- a user's choice to pin a
// book's cover to a specific PDF page or an uploaded image, overriding
// the auto-detected one -- as a flat JSON map keyed by source book path,
// under the same log_folder covercache.go already uses for cached cover
// images. Chosen over a sidecar file next to each book because it's
// consistent with the existing convention that log_folder is where all
// derived/cache state lives, and it doesn't require librarian.Scan or the
// rename/move pipeline to learn about a new file type living inside the
// organized library. Trade-off: an override won't travel if
// library_folder is copied to another machine without also copying
// log_folder.
package covercache

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// OverrideType distinguishes a page pinned from the book's own PDF pages
// ("embedded") from a user-uploaded replacement image ("custom").
type OverrideType string

const (
	OverrideEmbedded OverrideType = "embedded"
	OverrideCustom   OverrideType = "custom"
)

// Override is one book's manual cover choice. Page is meaningful only for
// OverrideEmbedded (1-based page number within the source PDF).
// ImagePath is meaningful only for OverrideCustom, and holds the already-
// served "/covers/..." URL of the uploaded image (see
// WriteCustomOverrideImage), not a filesystem path -- so callers can use
// it directly as CoverPath with no further resolution.
type Override struct {
	Type      OverrideType `json:"type"`
	Page      int          `json:"page,omitempty"`
	ImagePath string       `json:"imagePath,omitempty"`
}

func overridesPath(logFolder string) string {
	return filepath.Join(logFolder, "cover-overrides.json")
}

// loadOverrides reads the whole override map, treating a missing file as
// an empty map (no overrides set yet) rather than an error.
func loadOverrides(logFolder string) (map[string]Override, error) {
	data, err := os.ReadFile(overridesPath(logFolder))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]Override{}, nil
	}
	if err != nil {
		return nil, err
	}
	m := map[string]Override{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func saveOverrides(logFolder string, m map[string]Override) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(logFolder, 0755); err != nil {
		return err
	}
	return os.WriteFile(overridesPath(logFolder), data, 0644)
}

// GetOverride returns sourcePath's override, if one has been set.
func GetOverride(logFolder, sourcePath string) (Override, bool, error) {
	m, err := loadOverrides(logFolder)
	if err != nil {
		return Override{}, false, err
	}
	ov, ok := m[sourcePath]
	return ov, ok, nil
}

// SetOverride persists ov as sourcePath's override, replacing any
// existing one, via a whole-file read-modify-write.
func SetOverride(logFolder, sourcePath string, ov Override) error {
	m, err := loadOverrides(logFolder)
	if err != nil {
		return err
	}
	m[sourcePath] = ov
	return saveOverrides(logFolder, m)
}

// ClearOverride removes sourcePath's override (the "undo"). A no-op (not
// an error) if no override file exists yet, or none was set for
// sourcePath.
func ClearOverride(logFolder, sourcePath string) error {
	m, err := loadOverrides(logFolder)
	if err != nil {
		return err
	}
	delete(m, sourcePath)
	return saveOverrides(logFolder, m)
}
