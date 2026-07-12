package metadata

import (
	"os"
	"regexp"
	"strings"
	"unicode/utf16"

	"github.com/FrancisChung/book-organiser/internal/textutil"
)

var pdfLiteralStringRe = regexp.MustCompile(`/(Title|Author|Subject|CreationDate)\s*\(((?:[^()\\]|\\.)*)\)`)
var pdfDateYearRe = regexp.MustCompile(`D:(\d{4})`)
var pdfTrailerRe = regexp.MustCompile(`(?s)trailer\s*<<(.*?)>>`)
var pdfInfoRefRe = regexp.MustCompile(`/Info\s+(\d+)\s+\d+\s+R`)

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

// decodePDFString interprets already-unescaped PDF literal-string bytes per
// the spec's text-string rules: a leading 0xFE 0xFF byte-order mark means
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
// file. If the file has multiple trailers (incremental updates), the last
// one is used -- and, for the same "most recent update wins" reason, if
// object N itself was rewritten by an incremental update (common for
// PDFs edited by annotation/signing/metadata tools, which append rather
// than rewrite), the LAST "N ... obj ... endobj" block in the file is
// used too, not the first (now-superseded) one. Returns ok=false (caller
// falls back to a whole-file scan) if no trailer, no /Info reference, or
// no matching object is found -- preserving prior best-effort behavior
// for atypical PDFs (e.g. ones using cross-reference streams instead of a
// classic trailer) rather than erroring.
func findInfoDictBody(data []byte) ([]byte, bool) {
	trailers := pdfTrailerRe.FindAllSubmatch(data, -1)
	if len(trailers) == 0 {
		return nil, false
	}
	last := trailers[len(trailers)-1]
	infoMatch := pdfInfoRefRe.FindSubmatch(last[1])
	if infoMatch == nil {
		return nil, false
	}
	objNum := string(infoMatch[1])
	objRe := regexp.MustCompile(`(?s)\b` + objNum + `\s+\d+\s+obj(.*?)endobj`)
	objMatches := objRe.FindAllSubmatch(data, -1)
	if objMatches == nil {
		return nil, false
	}
	return objMatches[len(objMatches)-1][1], true
}

// extractPDF is a best-effort, dependency-free scanner: it looks for
// literal (not hex-encoded, not compressed-object-stream) /Title, /Author,
// /Subject, and /CreationDate entries in the raw PDF bytes, scoped to the
// document's real Info dictionary when it can be located (see
// findInfoDictBody). See the plan's Global Constraints for why this is
// deliberately not a full PDF parser.
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
func extractPDF(path string) (Result, error) {
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
	return result, nil
}
