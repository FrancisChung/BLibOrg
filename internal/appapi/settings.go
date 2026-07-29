// This file backs the Settings view's maintenance actions -- resetting
// the cover cache, and viewing/setting the library-scan concurrency.
package appapi

import (
	"fmt"
	"os"
	"runtime"

	"github.com/FrancisChung/BLibOrg/internal/config"
	"github.com/FrancisChung/BLibOrg/internal/covercache"
	"github.com/FrancisChung/BLibOrg/internal/librarycache"
)

// ResetCoverCache deletes every cached cover image and the persisted
// library scan cache, forcing every book to be treated as new on the next
// Scan. cover-overrides.json is untouched -- this fixes bad
// auto-detection, it doesn't discard deliberate choices made through the
// cover-override picker. Nothing is re-scanned here; the next Library
// view load or Refresh click naturally rebuilds from scratch since
// nothing is cached.
func (a *App) ResetCoverCache() error {
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	if err := os.RemoveAll(covercache.Dir(cfg.General.LogFolder)); err != nil {
		return err
	}
	return librarycache.Reset(cfg.General.LogFolder)
}

// ScanConcurrencyView is GetScanConcurrency's result: Configured is the
// raw cfg.General.ScanConcurrency value (0 means unset), Detected is
// runtime.NumCPU() -- the value librarian.Scan actually falls back to
// when Configured is 0. The Settings view pre-fills its input with
// Configured if it's > 0, else Detected, so the field always shows a
// concrete number rather than a blank/zero that reads as "unset."
type ScanConcurrencyView struct {
	Configured int `json:"configured"`
	Detected   int `json:"detected"`
}

// GetScanConcurrency reports the Settings view's current scan-concurrency
// state, for the concurrency input's pre-filled value.
func (a *App) GetScanConcurrency() (ScanConcurrencyView, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return ScanConcurrencyView{}, err
	}
	return ScanConcurrencyView{Configured: cfg.General.ScanConcurrency, Detected: runtime.NumCPU()}, nil
}

// SetScanConcurrency persists n as cfg.General.ScanConcurrency via a
// plain config.Load/config.Save round trip -- accepted as-is even though
// config.Save's yaml.Marshal strips comments and may reorder map keys,
// per this plan's Global Constraints. n == 0 means "auto" (see
// ScanConcurrencyView's doc comment); n < 0 is rejected outright, before
// the config is even loaded, since librarian.Scan's own "<= 0 means
// auto" convention would otherwise silently treat a negative typo the
// same as 0.
func (a *App) SetScanConcurrency(n int) error {
	if n < 0 {
		return fmt.Errorf("scan concurrency must be 0 or greater, got %d", n)
	}
	path, err := a.configPath()
	if err != nil {
		return err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	cfg.General.ScanConcurrency = n
	return config.Save(path, cfg)
}
