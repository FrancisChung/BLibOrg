package metadata

// Result holds whatever metadata an extractor could resolve. Any field left
// empty means "not found by this extractor" -- callers fall back accordingly.
type Result struct {
	Title   string
	Author  string
	Year    string
	Subject string

	// CoverBytes is the raw bytes of the first embedded cover image found
	// (EPUB manifest cover-image, MOBI/AZW3 PDB image record, PDF DCTDecode
	// image XObject -- see each extractor's doc comment), or nil if none was
	// found. Extraction is always best-effort: a missing cover is never an
	// error. CoverContentType is its MIME type (e.g. "image/jpeg"), set
	// whenever CoverBytes is non-empty.
	CoverBytes       []byte
	CoverContentType string
}
