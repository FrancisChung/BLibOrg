package metadata

import (
	"archive/zip"
	"encoding/xml"
	"fmt"

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
	} `xml:"metadata"`
}

func findZipFile(r *zip.ReadCloser, name string) (*zip.File, bool) {
	for _, f := range r.File {
		if f.Name == name {
			return f, true
		}
	}
	return nil, false
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
	return result, nil
}
