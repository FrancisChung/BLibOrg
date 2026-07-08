package metadata

import (
	"os"
	"regexp"

	"github.com/FrancisChung/book-organiser/internal/textutil"
)

var pdfLiteralStringRe = regexp.MustCompile(`/(Title|Author|Subject|CreationDate)\s*\(((?:[^()\\]|\\.)*)\)`)
var pdfDateYearRe = regexp.MustCompile(`D:(\d{4})`)

func unescapePDFString(s string) string {
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
	return string(out)
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
		fields[key] = unescapePDFString(string(m[2]))
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
