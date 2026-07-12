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
var edgeSepRe = regexp.MustCompile(`^[\s\-\x{2013}\x{2014}]+|[\s\-\x{2013}\x{2014}]+$`)

// looksLikeSingleName reports whether s, on its own, is shaped like a
// personal name: 2-4 words, each starting with a capital letter.
func looksLikeSingleName(s string) bool {
	return personNameRe.MatchString(s)
}

// looksLikeNameList reports whether s is a comma-separated list of two or
// more segments that each individually look like a personal name (e.g.
// "Bruce Eckel, Svetlana Isakova"). This is a stronger, more specific
// signal than looksLikeSingleName -- a lone 2-word phrase is often
// ambiguous with a short title ("Atomic Kotlin" reads just as name-shaped
// as "Bruce Eckel"), but a whole list of comma-joined name-shaped segments
// essentially never occurs as a book title.
func looksLikeNameList(s string) bool {
	parts := strings.Split(s, ",")
	if len(parts) < 2 {
		return false
	}
	for _, p := range parts {
		if !looksLikeSingleName(strings.TrimSpace(p)) {
			return false
		}
	}
	return true
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
	// A known junk tag (e.g. a trailing "- libgen.li") removed above can
	// leave its own adjacent " - " separator dangling at the very edge of
	// s once the tag text itself is gone. Trim that here, before the
	// title/author split, so the leftover hyphen doesn't get baked into
	// whichever field ends up owning that edge.
	s = edgeSepRe.ReplaceAllString(s, "")

	result := Result{Year: year}

	// Which side of "-" is the title and which is the author is ambiguous
	// from the filename alone -- two conventions are both common in the
	// wild: "Title - Author" (many source-site exports) and
	// "Author - Title[-Publisher]" (libgen/Pragmatic-Bookshelf-style). We
	// break the tie with two signals, most specific first: a comma-joined
	// list of name-shaped segments (looksLikeNameList) beats a lone 2-4
	// word name-shaped phrase (looksLikeSingleName), because a single
	// short phrase is often ambiguous with a short title ("Atomic Kotlin"
	// reads just as name-shaped as "Bruce Eckel") but a whole list of
	// comma-joined names essentially never is. If both sides tie on the
	// same signal, or neither matches, default to "Title - Author".
	parts := titleAuthorSepRe.Split(s, 2)
	if len(parts) == 2 {
		a := strings.TrimSpace(parts[0])
		b := strings.TrimSpace(parts[1])
		aList, bList := looksLikeNameList(a), looksLikeNameList(b)
		aSingle, bSingle := looksLikeSingleName(a), looksLikeSingleName(b)
		switch {
		case aList && !bList:
			result.Author, result.Title = a, b
		case bList && !aList:
			result.Title, result.Author = a, b
		case aSingle && !bSingle:
			result.Author, result.Title = a, b
		default:
			result.Title, result.Author = a, b
		}
	} else {
		result.Title = s
	}

	return result
}
