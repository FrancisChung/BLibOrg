// This file exposes the manual cover-override picker (design doc Section
// 4) to the Wails-bound frontend: listing every candidate image on a
// PDF's first N pages, pinning one, uploading a custom replacement, and
// undoing back to auto-detection.
package appapi

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/FrancisChung/book-organiser/internal/covercache"
	"github.com/FrancisChung/book-organiser/internal/metadata"
)

// CoverCandidateView is one selectable page/image for the cover-picker's
// thumbnail grid.
type CoverCandidateView struct {
	Page         int    `json:"page"`
	ThumbnailURL string `json:"thumbnailUrl"`
}

// ListPDFCoverCandidates returns every qualifying cover image found
// within the configured page limit of the PDF at bookPath, for the
// cover-override picker's thumbnail grid. A candidate this package fails
// to cache (covercache.WriteCandidateImage error) is silently omitted
// rather than failing the whole list -- matching this app's existing
// per-book best-effort convention (see librarian.Scan).
func (a *App) ListPDFCoverCandidates(bookPath string) ([]CoverCandidateView, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return nil, err
	}
	candidates, err := metadata.ListPDFCoverCandidates(bookPath, cfg.General.PDFCoverPageLimit)
	if err != nil {
		return nil, err
	}
	views := make([]CoverCandidateView, 0, len(candidates))
	for _, c := range candidates {
		url, err := covercache.WriteCandidateImage(cfg.General.LogFolder, bookPath, c.Page, c.Bytes, c.ContentType)
		if err != nil {
			continue
		}
		views = append(views, CoverCandidateView{Page: c.Page, ThumbnailURL: url})
	}
	return views, nil
}

// SetCoverOverride pins bookPath's cover to the image found on page,
// persists that choice (so future scans reuse it without re-prompting),
// and returns the resulting /covers/... URL immediately -- covercache.Force
// bypasses the mtime check Ensure would otherwise apply, since the source
// file's own mtime hasn't changed but the displayed cover must update
// right away.
func (a *App) SetCoverOverride(bookPath string, page int) (string, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return "", err
	}
	data, contentType, ok, err := metadata.ExtractPDFPageCover(bookPath, page)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("no qualifying image found on page %d", page)
	}
	if err := covercache.SetOverride(cfg.General.LogFolder, bookPath, covercache.Override{
		Type: covercache.OverrideEmbedded,
		Page: page,
	}); err != nil {
		return "", err
	}
	return covercache.Force(cfg.General.LogFolder, bookPath, data, contentType)
}

// SetCoverOverrideCustom pins bookPath's cover to an uploaded image,
// persisting it under log_folder/covers/ (covercache.WriteCustomOverrideImage)
// and recording the override so future scans reuse it without
// re-uploading.
func (a *App) SetCoverOverrideCustom(bookPath string, imageBytes []byte, contentType string) (string, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return "", err
	}
	url, err := covercache.WriteCustomOverrideImage(cfg.General.LogFolder, bookPath, imageBytes, contentType)
	if err != nil {
		return "", err
	}
	if err := covercache.SetOverride(cfg.General.LogFolder, bookPath, covercache.Override{
		Type:      covercache.OverrideCustom,
		ImagePath: url,
	}); err != nil {
		return "", err
	}
	return url, nil
}

// SetCoverOverrideCustomFromFile reads imagePath (from the frontend's
// native file-picker flow, see desktop/app.go's PickCoverImageFile) and
// delegates to SetCoverOverrideCustom -- kept separate so the byte-slice
// core stays directly unit-testable without a real file on disk.
func (a *App) SetCoverOverrideCustomFromFile(bookPath, imagePath string) (string, error) {
	data, err := os.ReadFile(imagePath)
	if err != nil {
		return "", err
	}
	return a.SetCoverOverrideCustom(bookPath, data, contentTypeFromExt(imagePath))
}

func contentTypeFromExt(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	default:
		return "image/jpeg"
	}
}

// ClearCoverOverride removes bookPath's override (the "undo") and re-runs
// normal auto-detection, returning the resulting URL -- which may be ""
// if extraction genuinely finds no cover.
func (a *App) ClearCoverOverride(bookPath string) (string, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return "", err
	}
	if err := covercache.ClearOverride(cfg.General.LogFolder, bookPath); err != nil {
		return "", err
	}
	res, err := metadata.Extract(bookPath, cfg.TitleFormatting.HyphenExceptions, cfg.General.PDFCoverPageLimit)
	if err != nil || len(res.CoverBytes) == 0 {
		return "", err
	}
	return covercache.Force(cfg.General.LogFolder, bookPath, res.CoverBytes, res.CoverContentType)
}
