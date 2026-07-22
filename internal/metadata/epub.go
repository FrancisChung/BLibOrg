package metadata

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"path"

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
		if cfile, found := findZipFile(r, coverZipPath); found {
			if crf, err := cfile.Open(); err == nil {
				if data, err := io.ReadAll(crf); err == nil {
					result.CoverBytes = data
					result.CoverContentType = mediaType
				}
				crf.Close()
			}
		}
	}

	return result, nil
}

func epubPathJoin(opfFullPath, href string) string {
	return path.Join(path.Dir(opfFullPath), href)
}
