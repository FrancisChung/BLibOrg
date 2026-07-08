package textutil

import "regexp"

var yearRe = regexp.MustCompile(`\b(1[5-9]\d{2}|20\d{2})\b`)

// ExtractYear pulls the first plausible 4-digit year (1500-2099) out of s,
// bounded by non-word characters on both sides. It will not find a year
// inside a longer unbroken digit run (e.g. a PDF "D:20240101103000" date
// string) -- callers dealing with that specific format should match it
// directly before falling back to this.
func ExtractYear(s string) (string, bool) {
	m := yearRe.FindString(s)
	if m == "" {
		return "", false
	}
	return m, true
}
