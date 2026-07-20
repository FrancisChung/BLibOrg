package textutil

import (
	"regexp"
	"strings"
)

var separatorRunRe = regexp.MustCompile(`\s*;\s*`)

// CleanField trims a trailing run of ".", ";", and whitespace from s. Titles
// and authors pulled from embedded book metadata or filename heuristics
// occasionally carry one of these as leftover punctuation (e.g. a sentence
// fragment or a dangling list separator with nothing after it); neither is
// ever meaningful at the very end of a title or author.
func CleanField(s string) string {
	return strings.TrimRight(s, " \t.;")
}

// NormalizeAuthorSeparators rewrites ";"-separated author lists (as some
// sources emit for multiple authors) into the ","-separated form used
// elsewhere in the app.
func NormalizeAuthorSeparators(s string) string {
	return separatorRunRe.ReplaceAllString(s, ", ")
}
