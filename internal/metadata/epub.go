package metadata

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"

	"github.com/FrancisChung/book-organiser/internal/textutil"
)

type epubContainer struct {
	Rootfiles struct {
		Rootfile struct {
			FullPath string `xml:"full-path,attr"`
		} `xml:"rootfile"`
	} `xml:"rootfiles"`
}

type epubPackage struct {
	Metadata struct {
		Title   string `xml:"title"`
		Creator string `xml:"creator"`
		Date    string `xml:"date"`
		Subject string `xml:"subject"`
		Meta    []struct {
			Name    string `xml:"name,attr"`
			Content string `xml:"content,attr"`
		} `xml:"meta"`
	} `xml:"metadata"`
	Manifest struct {
		Items []struct {
			ID         string `xml:"id,attr"`
			Href       string `xml:"href,attr"`
			MediaType  string `xml:"media-type,attr"`
			Properties string `xml:"properties,attr"`
		} `xml:"item"`
	} `xml:"manifest"`
	Spine struct {
		ItemRefs []struct {
			IDRef string `xml:"idref,attr"`
		} `xml:"itemref"`
	} `xml:"spine"`
}

var epubImgSrcRe = regexp.MustCompile(`(?i)<img[^>]*\ssrc=["']([^"']*)["']`)

var epubPlaceholderTitleRe = regexp.MustCompile(`^[0-9]+$`)

var epubPlaceholderAuthors = map[string]bool{
	"unknown": true,
}

func findZipFile(r *zip.ReadCloser, name string) (*zip.File, bool) {
	for _, f := range r.File {
		if f.Name == name {
			return f, true
		}
	}
	return nil, false
}

// findEpubCoverItem returns the href/media-type of p's cover image, checked
// in priority order: the EPUB3 convention (a manifest <item> whose
// properties list includes "cover-image") first, falling back to the EPUB2
// convention (a <meta name="cover" content="ITEM_ID"/> pointing at a
// manifest item by id). Returns ok=false if neither convention is present.
func findEpubCoverItem(p epubPackage) (href, mediaType string, ok bool) {
	for _, item := range p.Manifest.Items {
		for _, prop := range splitEpubProperties(item.Properties) {
			if prop == "cover-image" {
				return item.Href, item.MediaType, true
			}
		}
	}

	var coverID string
	for _, m := range p.Metadata.Meta {
		if m.Name == "cover" {
			coverID = m.Content
			break
		}
	}
	if coverID == "" {
		return "", "", false
	}
	for _, item := range p.Manifest.Items {
		if item.ID == coverID {
			return item.Href, item.MediaType, true
		}
	}
	return "", "", false
}

func splitEpubProperties(properties string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(properties); i++ {
		if i == len(properties) || properties[i] == ' ' {
			if i > start {
				out = append(out, properties[start:i])
			}
			start = i + 1
		}
	}
	return out
}

// findEpubFirstSpineImage is the fallback used when findEpubCoverItem
// finds neither the EPUB3 nor EPUB2 cover convention: many older,
// malformed, or auto-converted EPUBs (e.g. a calibre conversion with no
// cover metadata at all) still put the cover image alone on an
// otherwise-empty first page, so the first <img> tag in the first spine
// document is, in practice, the cover. Unlike findEpubCoverItem, this
// returns the fully-resolved in-zip path (not an OPF-relative href),
// since the image's path is computed relative to the spine document's
// own location, which may differ from the OPF's. Returns ok=false if the
// spine is empty, its first item can't be resolved to a manifest href,
// that document can't be opened, or it contains no <img> tag at all.
func findEpubFirstSpineImage(r *zip.ReadCloser, opfFullPath string, p epubPackage) (zipPath, mediaType string, ok bool) {
	if len(p.Spine.ItemRefs) == 0 {
		return "", "", false
	}
	firstID := p.Spine.ItemRefs[0].IDRef
	var spineHref string
	for _, item := range p.Manifest.Items {
		if item.ID == firstID {
			spineHref = item.Href
			break
		}
	}
	if spineHref == "" {
		return "", "", false
	}
	spineZipPath := epubPathJoin(opfFullPath, spineHref)
	sf, found := findZipFile(r, spineZipPath)
	if !found {
		return "", "", false
	}
	src, err := sf.Open()
	if err != nil {
		return "", "", false
	}
	defer src.Close()
	data, err := io.ReadAll(src)
	if err != nil {
		return "", "", false
	}
	m := epubImgSrcRe.FindSubmatch(data)
	if m == nil {
		return "", "", false
	}
	imgZipPath := epubPathJoin(spineZipPath, string(m[1]))

	for _, item := range p.Manifest.Items {
		if item.MediaType != "" && epubPathJoin(opfFullPath, item.Href) == imgZipPath {
			return imgZipPath, item.MediaType, true
		}
	}
	if guessed := epubGuessMediaType(imgZipPath); guessed != "" {
		return imgZipPath, guessed, true
	}
	return "", "", false
}

// epubGuessMediaType infers a media type from a zip path's file
// extension, for the rare case findEpubFirstSpineImage locates an <img>
// tag whose target isn't itself declared in the manifest (a more
// malformed EPUB than the manifest-declared common case). Returns "" for
// an unrecognized extension, so callers can treat that as "give up"
// rather than guessing wrong.
func epubGuessMediaType(zipPath string) string {
	switch strings.ToLower(path.Ext(zipPath)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	default:
		return ""
	}
}

// readEpubCoverBytes opens zipPath within r and, if successful, sets
// result.CoverBytes/CoverContentType -- the shared "open, read, assign"
// step both findEpubCoverItem's result and findEpubFirstSpineImage's
// fallback result go through once a candidate cover has been located.
func readEpubCoverBytes(r *zip.ReadCloser, zipPath, mediaType string, result *Result) {
	cfile, found := findZipFile(r, zipPath)
	if !found {
		return
	}
	crf, err := cfile.Open()
	if err != nil {
		return
	}
	defer crf.Close()
	data, err := io.ReadAll(crf)
	if err != nil {
		return
	}
	result.CoverBytes = data
	result.CoverContentType = mediaType
}

func extractEpub(path string) (Result, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return Result{}, err
	}
	defer r.Close()

	cf, ok := findZipFile(r, "META-INF/container.xml")
	if !ok {
		return Result{}, fmt.Errorf("epub missing META-INF/container.xml")
	}
	crc, err := cf.Open()
	if err != nil {
		return Result{}, err
	}
	defer crc.Close()
	var c epubContainer
	if err := xml.NewDecoder(crc).Decode(&c); err != nil {
		return Result{}, err
	}

	of, ok := findZipFile(r, c.Rootfiles.Rootfile.FullPath)
	if !ok {
		return Result{}, fmt.Errorf("epub missing opf file %s", c.Rootfiles.Rootfile.FullPath)
	}
	orc, err := of.Open()
	if err != nil {
		return Result{}, err
	}
	defer orc.Close()
	var p epubPackage
	if err := xml.NewDecoder(orc).Decode(&p); err != nil {
		return Result{}, err
	}

	result := Result{
		Title:   p.Metadata.Title,
		Author:  p.Metadata.Creator,
		Subject: p.Metadata.Subject,
	}
	if epubPlaceholderTitleRe.MatchString(strings.TrimSpace(result.Title)) {
		result.Title = ""
	}
	if epubPlaceholderAuthors[strings.ToLower(strings.TrimSpace(result.Author))] {
		result.Author = ""
	}
	if year, ok := textutil.ExtractYear(p.Metadata.Date); ok {
		result.Year = year
	}

	if href, mediaType, ok := findEpubCoverItem(p); ok {
		// href is relative to the OPF's own directory, not the zip root
		// (e.g. opf at "OEBPS/content.opf", href "images/cover.jpg" ->
		// "OEBPS/images/cover.jpg"). Zip entry names always use "/", so this
		// must use the "path" package, not "path/filepath" (which uses "\"
		// on Windows and would silently fail to match).
		coverZipPath := epubPathJoin(c.Rootfiles.Rootfile.FullPath, href)
		readEpubCoverBytes(r, coverZipPath, mediaType, &result)
	} else if zipPath, mediaType, ok := findEpubFirstSpineImage(r, c.Rootfiles.Rootfile.FullPath, p); ok {
		readEpubCoverBytes(r, zipPath, mediaType, &result)
	}

	return result, nil
}

func epubPathJoin(opfFullPath, href string) string {
	return path.Join(path.Dir(opfFullPath), href)
}
