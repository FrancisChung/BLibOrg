// This file backs the Settings view's maintenance actions -- currently
// just resetting the cover cache.
package appapi

import (
	"os"

	"github.com/FrancisChung/book-organiser/internal/covercache"
	"github.com/FrancisChung/book-organiser/internal/librarycache"
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
