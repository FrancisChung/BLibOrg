package duplicates

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/FrancisChung/book-organiser/internal/book"
)

// normalize lowercases s and collapses any run of characters that are not
// Unicode letters or digits into a single space, then trims leading/
// trailing whitespace. Using unicode.IsLetter/IsDigit (rather than an
// ASCII-only [a-z0-9] regex) ensures accented and non-Latin letters (e.g.
// "García", "Müller") are preserved rather than stripped out as separators.
func normalize(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevSpace = false
		} else if !prevSpace {
			b.WriteRune(' ')
			prevSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

// sizeWithinTolerance reports whether two file sizes are close enough to be
// the same underlying file: within 1% of the larger size, or within 1024
// bytes, whichever is the larger allowance. This tolerance is an
// implementation default (not specified numerically in the design spec).
func sizeWithinTolerance(a, b int64) bool {
	if a == 0 || b == 0 {
		return a == b
	}
	larger, smaller := a, b
	if smaller > larger {
		larger, smaller = smaller, larger
	}
	diff := larger - smaller
	tolerance := larger / 100
	if tolerance < 1024 {
		tolerance = 1024
	}
	return diff <= tolerance
}

// Detect groups books by normalized title+author (case/punctuation-
// insensitive exact match -- not fuzzy/edit-distance matching, see the
// plan's Global Constraints). Within any group of 2+ books, each member's
// status reflects its own pairwise relationships within the group: a book
// is LikelyDuplicate if it has at least one same-format match whose size is
// within tolerance against another member of the group; otherwise it is
// PossibleDuplicate. This is evaluated per book rather than once for the
// whole group, so e.g. in a 3-member group where only two members share
// format+size, those two are LikelyDuplicate while the third (which matches
// neither on format nor size) is PossibleDuplicate -- all three still share
// the same DuplicateGroupID. Books with neither title nor author resolved
// are never grouped. Mutates books in place.
func Detect(books []*book.Book) {
	groups := map[string][]*book.Book{}
	for _, b := range books {
		key := normalize(b.Title.Value) + "|" + normalize(b.Author.Value)
		if key == "|" {
			continue
		}
		groups[key] = append(groups[key], b)
	}

	groupNum := 0
	for _, members := range groups {
		if len(members) < 2 {
			continue
		}
		groupNum++
		groupID := fmt.Sprintf("dup-%d", groupNum)

		likely := make([]bool, len(members))
		for i := 0; i < len(members); i++ {
			for j := i + 1; j < len(members); j++ {
				if members[i].Format == members[j].Format && sizeWithinTolerance(members[i].SizeBytes, members[j].SizeBytes) {
					likely[i] = true
					likely[j] = true
				}
			}
		}
		for i, m := range members {
			m.DuplicateGroupID = groupID
			if likely[i] {
				m.DuplicateStatus = book.LikelyDuplicate
			} else {
				m.DuplicateStatus = book.PossibleDuplicate
			}
		}
	}
}
