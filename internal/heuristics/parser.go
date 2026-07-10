package heuristics

import (
	"regexp"
	"strings"

	"github.com/FrancisChung/book-organiser/internal/textutil"
)

type Result struct {
	Title  string
	Author string
	Year   string
}

var bracketedRe = regexp.MustCompile(`[\(\{\[][^)\}\]]*[\)\}\]]`)
var delimiterRunRe = regexp.MustCompile(`[+_.]+`)
var whitespaceRunRe = regexp.MustCompile(`\s+`)
var titleAuthorSepRe = regexp.MustCompile(`\s+-\s+`)
var personNameRe = regexp.MustCompile(`^[A-Z][a-zA-Z'.-]*(\s+[A-Z][a-zA-Z'.-]*){1,3}$`)

// looksLikePersonName reports whether s is shaped like a personal name: 2-4
// words, each starting with a capital letter. It's the signal used to
// disambiguate which side of a "-" separator is the author.
func looksLikePersonName(s string) bool {
	return personNameRe.MatchString(s)
}

// Parse applies best-effort heuristics to a bare filename stem (no
// extension, no directory) to guess title/author/year. It is intentionally
// conservative: bracketed content is stripped wholesale as likely junk
// (release-group tags, database IDs, publisher blurbs) even though this
// occasionally removes a real author that happened to be bracketed --
// callers must treat these results as a fallback that needs human review,
// per the design spec's mandatory View 1 manual-edit requirement.
func Parse(filenameStem string, knownJunkTags []string) Result {
	s := filenameStem

	for _, tag := range knownJunkTags {
		tagRe := regexp.MustCompile(`(?i)_?` + regexp.QuoteMeta(tag) + `_?`)
		s = tagRe.ReplaceAllString(s, " ")
	}

	year, _ := textutil.ExtractYear(s)

	s = bracketedRe.ReplaceAllString(s, " ")
	s = delimiterRunRe.ReplaceAllString(s, " ")
	s = whitespaceRunRe.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)

	result := Result{Year: year}

	// Which side of "-" is the title and which is the author is ambiguous
	// from the filename alone -- two conventions are both common in the
	// wild: "Title - Author" (many source-site exports) and
	// "Author - Title[-Publisher]" (libgen/Pragmatic-Bookshelf-style). We
	// break the tie by checking which side reads like a 2-4 word personal
	// name: that side is the Author, the other is the Title. If both or
	// neither look like a name, default to "Title - Author" as before.
	parts := titleAuthorSepRe.Split(s, 2)
	if len(parts) == 2 {
		a := strings.TrimSpace(parts[0])
		b := strings.TrimSpace(parts[1])
		if looksLikePersonName(a) && !looksLikePersonName(b) {
			result.Author = a
			result.Title = b
		} else {
			result.Title = a
			result.Author = b
		}
	} else {
		result.Title = s
	}

	return result
}
