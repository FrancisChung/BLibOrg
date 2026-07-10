package metadata

import (
	"os"
	"regexp"
	"unicode/utf16"

	"github.com/FrancisChung/book-organiser/internal/textutil"
)

var pdfLiteralStringRe = regexp.MustCompile(`/(Title|Author|Subject|CreationDate)\s*\(((?:[^()\\]|\\.)*)\)`)
var pdfDateYearRe = regexp.MustCompile(`D:(\d{4})`)

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

// extractPDF is a best-effort, dependency-free scanner: it looks for
// literal (not hex-encoded, not compressed-object-stream) /Title, /Author,
// /Subject, and /CreationDate entries in the raw PDF bytes. See the plan's
// Global Constraints for why this is deliberately not a full PDF parser.
func extractPDF(path string) (Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, err
	}

	fields := map[string]string{}
	for _, m := range pdfLiteralStringRe.FindAllSubmatch(data, -1) {
		key := string(m[1])
		if _, exists := fields[key]; exists {
			continue // keep first match only
		}
		fields[key] = decodePDFString(unescapePDFBytes(string(m[2])))
	}

	result := Result{
		Title:   fields["Title"],
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
