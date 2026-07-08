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

	// "Title - Author" is ambiguous from the filename alone; v1 treats the
	// first segment as Title and the second as Author, matching both this
	// tool's own output convention and common source-site naming.
	parts := titleAuthorSepRe.Split(s, 2)
	if len(parts) == 2 {
		result.Title = strings.TrimSpace(parts[0])
		result.Author = strings.TrimSpace(parts[1])
	} else {
		result.Title = s
	}

	return result
}
