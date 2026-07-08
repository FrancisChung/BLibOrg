package duplicates

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/FrancisChung/book-organiser/internal/book"
)

var normalizeRe = regexp.MustCompile(`[^a-z0-9]+`)

func normalize(s string) string {
	s = strings.ToLower(s)
	s = normalizeRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
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
// plan's Global Constraints). Within any group of 2+ books, same-format
// pairs whose sizes are within tolerance make the whole group
// LikelyDuplicate; otherwise the group is PossibleDuplicate. Books with
// neither title nor author resolved are never grouped. Mutates books in
// place.
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

		anyLikely := false
		for i := 0; i < len(members); i++ {
			for j := i + 1; j < len(members); j++ {
				if members[i].Format == members[j].Format && sizeWithinTolerance(members[i].SizeBytes, members[j].SizeBytes) {
					anyLikely = true
				}
			}
		}
		status := book.PossibleDuplicate
		if anyLikely {
			status = book.LikelyDuplicate
		}
		for _, m := range members {
			m.DuplicateGroupID = groupID
			m.DuplicateStatus = status
		}
	}
}
