package metadata

import (
	"bytes"
	"encoding/hex"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/FrancisChung/book-organiser/internal/textutil"
)

var pdfLiteralStringRe = regexp.MustCompile(`/(Title|Author|Subject|CreationDate)\s*\(((?:[^()\\]|\\.)*)\)`)
var pdfHexStringRe = regexp.MustCompile(`/(Title|Author|Subject|CreationDate)\s*<([0-9A-Fa-f\s]*)>`)
var pdfDateYearRe = regexp.MustCompile(`D:(\d{4})`)
var pdfTrailerRe = regexp.MustCompile(`(?s)trailer\s*<<(.*?)>>`)
var pdfInfoRefRe = regexp.MustCompile(`/Info\s+(\d+)\s+\d+\s+R`)
var pdfXRefTypeRe = regexp.MustCompile(`/Type\s*/XRef\b`)
var pdfImageStreamRe = regexp.MustCompile(`(?s)<<([^>]*?)>>\s*stream\r?\n(.*?)endstream`)
var pdfSubtypeImageRe = regexp.MustCompile(`/Subtype\s*/Image`)
var pdfDCTDecodeRe = regexp.MustCompile(`/Filter\s*/DCTDecode|/Filter\s*\[[^\]]*/DCTDecode`)

// placeholderTitles are literal /Title values some PDF-generation tools
// leave behind as a default when the real author never set a document
// title. Reporting one of these as resolved SourceMetadata would silently
// block the filename heuristic parser from ever running for Title (it
// only runs for fields that come back empty), so they're treated the same
// as "not found."
var placeholderTitles = map[string]bool{
	"untitled": true,
}

// unescapePDFBytes resolves PDF literal-string backslash escapes on the raw
// byte stream. This happens before any character-encoding interpretation --
// per the PDF spec, escapes are a byte-level syntax feature independent of
// whether the resulting bytes are later read as PDFDocEncoding or UTF-16BE.
func unescapePDFBytes(s string) []byte {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			default:
				out = append(out, s[i])
			}
			continue
		}
		out = append(out, s[i])
	}
	return out
}

// decodePDFHexBytes decodes h (the content of a PDF hex string, between
// its enclosing < and >) into raw bytes: whitespace and any non-hex
// character are stripped first (PDF permits whitespace inside a hex
// string), then an odd number of remaining digits gets an implicit
// trailing "0" appended per the spec (e.g. <901FA> means <901FA0>)
// before pairing. Sibling to unescapePDFBytes, which does the equivalent
// job for literal-string source syntax -- both feed their raw-byte
// result into decodePDFString for the shared encoding-detection step.
func decodePDFHexBytes(h []byte) []byte {
	digits := make([]byte, 0, len(h))
	for _, b := range h {
		if (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F') {
			digits = append(digits, b)
		}
	}
	if len(digits)%2 != 0 {
		digits = append(digits, '0')
	}
	decoded, err := hex.DecodeString(string(digits))
	if err != nil {
		return nil
	}
	return decoded
}

// decodePDFString interprets already-decoded PDF text-string bytes (from
// either literal-string syntax via unescapePDFBytes, or hex-string syntax
// via decodePDFHexBytes -- both produce the same raw-byte shape) per the
// spec's text-string rules: a leading 0xFE 0xFF byte-order mark means
// the remainder is UTF-16BE -- the encoding real-world producers (Chromium
// print-to-PDF, calibre) use for any /Title or /Author containing non-ASCII
// characters. Without that BOM, bytes are PDFDocEncoding/WinAnsiEncoding,
// which real-world producers overwhelmingly use as a byte-identical superset
// of Latin-1 (ISO-8859-1) -- decoding byte-for-byte as Latin-1 (every byte
// maps directly to the identically-numbered Unicode code point) is exact for
// plain ASCII and correct for the common accented-Latin-character case (e.g.
// 0xF6 -> 'ö'); it does not implement PDFDocEncoding's few non-Latin-1
// mappings in the 0x18-0x1F and 0x80-0x9F ranges (rare in practice for
// title/author text), matching this package's dependency-free, best-effort
// scope. Critically, this always produces valid UTF-8 -- the prior behavior
// of passing raw high-byte bytes straight into a Go string did not, and
// broke file rename/copy syscalls on any filesystem that validates UTF-8.
func decodePDFString(raw []byte) string {
	if len(raw) >= 2 && raw[0] == 0xFE && raw[1] == 0xFF {
		body := raw[2:]
		units := make([]uint16, len(body)/2)
		for i := range units {
			units[i] = uint16(body[2*i])<<8 | uint16(body[2*i+1])
		}
		return string(utf16.Decode(units))
	}
	runes := make([]rune, len(raw))
	for i, b := range raw {
		runes[i] = rune(b)
	}
	return string(runes)
}

// findInfoDictBody locates the byte range of the PDF's real Info
// dictionary object, via the trailer's authoritative "/Info N 0 R"
// reference, so metadata extraction reads only the document's actual
// Title/Author/Subject/CreationDate instead of whichever matching pattern
// happens to appear first anywhere in the file. This matters because PDFs
// commonly embed graphics (logos, diagrams) that carry their own /Title,
// /Author, /Creator describing that graphic -- e.g. a CorelDRAW logo's own
// /Title, or an Illustrator diagram's own /Author -- and a naive
// first-match-anywhere scan can pick up a graphic's metadata instead of
// the book's if that graphic's object happens to appear earlier in the
// file. If the file has multiple classic trailers, the last one that
// actually declares /Info is used (see findTrailerDictWithInfo) -- not
// simply the byte-order-last trailer, since a linearized PDF's tail
// trailer commonly lacks /Info entirely. The object lookup below resolves
// via pdfObjIndex.lookup, which already prefers the LAST literal
// occurrence of a given object number (see buildPDFObjIndex) and
// additionally resolves an object compressed inside a /Type /ObjStm
// container (common in XeTeX/LaTeX-produced PDFs, which never appears as
// literal "N ... obj ... endobj" text in the file at all). If no classic
// trailer declares /Info at all, falls back to findXRefStreamTrailerDict
// for PDFs using a PDF 1.5+ cross-reference stream instead (which carries
// the same /Info key directly in its own dictionary). Returns ok=false
// (caller falls back to a whole-file scan) if neither a trailer nor an
// XRef stream trailer-equivalent, no /Info reference, or no matching
// object is found -- preserving prior best-effort behavior for atypical
// PDFs rather than erroring.
func findInfoDictBody(data []byte) ([]byte, bool) {
	idx := buildPDFObjIndex(data)
	trailerDict := findTrailerDictWithInfo(data)
	if trailerDict == nil {
		trailerDict = findXRefStreamTrailerDict(idx)
		if trailerDict == nil {
			return nil, false
		}
	}
	infoMatch := pdfInfoRefRe.FindSubmatch(trailerDict)
	if infoMatch == nil {
		return nil, false
	}
	objNum, err := strconv.Atoi(string(infoMatch[1]))
	if err != nil {
		return nil, false
	}
	body, ok := idx.lookup(objNum)
	if !ok {
		return nil, false
	}
	dict, _, _ := splitPDFObjectBody(body)
	return dict, true
}

// findTrailerDictWithInfo scans classic "trailer <<...>>" blocks in file
// order and returns the LAST one that actually declares an /Info key --
// not simply the byte-order-last trailer. A linearized PDF's tail
// trailer (associated with the base/original cross-reference table
// preserved near the end of the file) is commonly stripped of /Root and
// /Info by the linearization tool, while the first trailer in file order
// (prepended for fast web view) is the complete, authoritative one --
// blindly trusting "last in file order" would pick the wrong one.
// Scanning in reverse and returning the first /Info-bearing match found
// preserves "last wins" for the already-supported genuine
// incremental-update case (where every trailer has /Info, and the reverse
// scan's first hit IS the true last one), while correctly skipping a
// linearized tail trailer that lacks /Info. Returns nil if no classic
// trailer declares /Info at all, or if data has no classic trailer.
func findTrailerDictWithInfo(data []byte) []byte {
	trailers := pdfTrailerRe.FindAllSubmatch(data, -1)
	for i := len(trailers) - 1; i >= 0; i-- {
		if pdfInfoRefRe.Match(trailers[i][1]) {
			return trailers[i][1]
		}
	}
	return nil
}

// findXRefStreamTrailerDict locates the trailer-equivalent dict for PDFs
// using a PDF 1.5+ cross-reference stream instead of a classic "trailer
// <<...>>" keyword block: the XRef stream object (/Type /XRef) carries
// the same /Root, /Info, /Size keys directly in its own dictionary, per
// the PDF spec. Returns the LAST such object's dict in file order
// (mirroring findInfoDictBody's "most recent update wins" handling for
// classic trailers, since a later XRef stream supersedes an earlier
// one), or nil if no /Type /XRef object exists anywhere in idx. Takes an
// already-built *pdfObjIndex (from findInfoDictBody, its only caller)
// rather than building its own, avoiding a second full-file re-scan.
func findXRefStreamTrailerDict(idx *pdfObjIndex) []byte {
	objNums := make([]int, 0, len(idx.literal))
	for n := range idx.literal {
		objNums = append(objNums, n)
	}
	sort.Slice(objNums, func(i, j int) bool {
		return idx.literalOrder[objNums[i]] < idx.literalOrder[objNums[j]]
	})
	var found []byte
	for _, n := range objNums {
		dict, _, _ := splitPDFObjectBody(idx.literal[n])
		if pdfXRefTypeRe.Match(dict) {
			found = dict
		}
	}
	return found
}

// findPDFCover scans data for the first qualifying image XObject stream
// (a "<<...>>\nstream ... endstream" block whose dictionary declares
// /Subtype /Image) and returns its display-ready bytes: a /Filter
// /DCTDecode stream's raw bytes are already a complete, valid JPEG, per
// the PDF spec; any other filter is attempted via decodeFlatePDFImage
// (pdf_flate.go), passing resources=nil since this whole-file scan has
// no page context to resolve a named colorspace against -- inline and
// indirect colorspaces (the common case) still resolve fine. This is a
// textual scan, not a real PDF parser: it does not handle a dictionary
// containing its own nested "<<...>>" (e.g. a /DecodeParms
// sub-dictionary), matching the rest of this file's deliberately
// best-effort approach.
func findPDFCover(idx *pdfObjIndex, data []byte) ([]byte, string, bool) {
	for _, m := range pdfImageStreamRe.FindAllSubmatch(data, -1) {
		dict := m[1]
		if !pdfSubtypeImageRe.Match(dict) {
			continue
		}
		stream := bytes.TrimRight(m[2], "\r\n")
		if len(stream) == 0 {
			continue
		}
		if pdfDCTDecodeRe.Match(dict) {
			return stream, "image/jpeg", true
		}
		if flateBytes, contentType, ok := decodeFlatePDFImage(idx, nil, dict, stream); ok {
			return flateBytes, contentType, true
		}
	}
	return nil, "", false
}

// renderPDFPageAsCoverFunc is a seam so tests can verify whether
// full-page rendering was invoked without a real PDFium call in every
// test; production code always uses renderPDFPageAsCover (pdf_render.go).
var renderPDFPageAsCoverFunc = renderPDFPageAsCover

// findPDFCoverPageAware is the primary cover-selection entry point: it
// walks the page tree (walkPDFPageTree, pdf_pages.go) and returns the
// first qualifying image found within the first pageLimit pages, in page
// order. If that image's page also shows signs of being a composite
// cover -- a text-show operator alongside the image, per
// pageContentSuggestsCompositeCover -- the whole page is rendered in
// full instead (renderPDFPageAsCover, pdf_render.go), falling back to the
// raw image if rendering fails. If the page tree can't be resolved at
// all, or no qualifying image turns up within the page limit, it falls
// back to findPDFCover's whole-file byte-order scan -- so this is never
// worse than the pre-page-aware behavior, only better when a real page
// tree is present.
func findPDFCoverPageAware(data []byte, pageLimit int) ([]byte, string, bool) {
	idx := buildPDFObjIndex(data)
	if pages, ok := walkPDFPageTree(idx, pageLimit); ok {
		if images := findPDFPageImages(idx, pages, true); len(images) > 0 {
			img := images[0]
			if page, found := findPageByNumber(pages, img.page); found && pageContentSuggestsCompositeCover(idx, page) {
				if renderedBytes, renderedContentType, ok := renderPDFPageAsCoverFunc(data, img.page); ok {
					return renderedBytes, renderedContentType, true
				}
			}
			return img.bytes, img.contentType, true
		}
	}
	return findPDFCover(idx, data)
}

// findPageByNumber returns the page in pages whose 1-based number matches,
// for looking up the page a candidate image was found on (pdfPageImage
// only carries the page number, not the full pdfPage, since
// findPDFPageImages predates this file's need for it).
func findPageByNumber(pages []pdfPage, number int) (pdfPage, bool) {
	for _, p := range pages {
		if p.number == number {
			return p, true
		}
	}
	return pdfPage{}, false
}

// extractPDF is a best-effort, dependency-free scanner: it looks for
// /Title, /Author, /Subject, and /CreationDate entries -- written as
// either literal strings ("(...)") or hex strings ("<...>"), and whether
// the Info dictionary itself is a standalone object or compressed inside
// an ObjStm -- in the raw PDF bytes, scoped to the document's real Info
// dictionary when it can be located (see findInfoDictBody). See the
// plan's Global Constraints for why this is deliberately not a full PDF
// parser.
//
// When the real Info dictionary can't be located, Title and Author are left
// unset rather than falling back to a whole-file scan: real books commonly
// contain many OTHER objects with their own /Title (outline/bookmark
// entries -- one per chapter/section -- and embedded graphics) or /Author
// (embedded graphics' creator), and a whole-file scan has no way to tell
// those apart from the real book's metadata. Confidently reporting a
// bookmark's title as "Metadata" is worse than reporting nothing, because it
// silently blocks the filename heuristic parser from ever running (it only
// runs for fields that come back empty). Subject and CreationDate remain
// safe to whole-file-scan even without a confirmed Info dict: unlike
// /Title/Author, those two keys essentially never appear on bookmark or
// embedded-graphic objects in practice.
func extractPDF(path string, pageLimit int) (Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, err
	}

	scope, foundInfo := findInfoDictBody(data)
	if !foundInfo {
		scope = data
	}

	fields := map[string]string{}
	for _, m := range pdfLiteralStringRe.FindAllSubmatch(scope, -1) {
		key := string(m[1])
		if !foundInfo && (key == "Title" || key == "Author") {
			continue
		}
		if _, exists := fields[key]; exists {
			continue // keep first match only
		}
		fields[key] = decodePDFString(unescapePDFBytes(string(m[2])))
	}
	for _, m := range pdfHexStringRe.FindAllSubmatch(scope, -1) {
		key := string(m[1])
		if !foundInfo && (key == "Title" || key == "Author") {
			continue
		}
		if _, exists := fields[key]; exists {
			continue // literal-string match (if any) takes precedence
		}
		fields[key] = decodePDFString(decodePDFHexBytes(m[2]))
	}

	title := fields["Title"]
	if placeholderTitles[strings.ToLower(strings.TrimSpace(title))] {
		title = ""
	}

	result := Result{
		Title:   title,
		Author:  fields["Author"],
		Subject: fields["Subject"],
	}
	if m := pdfDateYearRe.FindStringSubmatch(fields["CreationDate"]); m != nil {
		result.Year = m[1]
	} else if year, ok := textutil.ExtractYear(fields["CreationDate"]); ok {
		result.Year = year
	}
	if coverData, coverContentType, ok := findPDFCoverPageAware(data, pageLimit); ok {
		result.CoverBytes = coverData
		result.CoverContentType = coverContentType
	}
	return result, nil
}
