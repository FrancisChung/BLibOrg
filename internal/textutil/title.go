package textutil

import (
	"regexp"
	"strings"
	"unicode"
)

var hyphenRunRe = regexp.MustCompile(`[A-Za-z0-9]+(?:-[A-Za-z0-9]+)+`)
var wordCoreRe = regexp.MustCompile(`^[^a-zA-Z]*([a-zA-Z]+)[^a-zA-Z]*$`)

// smallWords are the Chicago-style articles, coordinating conjunctions, and
// short prepositions that FormatTitle lowercases unless they open or close
// the title.
var smallWords = map[string]bool{
	"a": true, "an": true, "and": true, "as": true, "at": true, "but": true,
	"by": true, "en": true, "for": true, "from": true, "if": true, "in": true,
	"into": true, "is": true, "nor": true, "of": true, "off": true, "on": true,
	"onto": true, "or": true, "out": true, "over": true, "per": true, "so": true,
	"than": true, "that": true, "the": true, "to": true, "up": true, "via": true,
	"vs": true, "when": true, "with": true, "yet": true,
}

// FormatTitle converts "_" to spaces, converts "-" to spaces except within a
// hyphenExceptions entry (matched case-insensitively, substituted with that
// entry's exact stored casing -- e.g. "high-performance" stays hyphenated as
// "High-Performance" rather than becoming "High Performance"), then applies
// Chicago-style Title Case: small words (see smallWords) are lowercased
// unless they open or close the title, and a word that already contains an
// uppercase letter past its first letter (e.g. "iOS", "DevOps", "API") is
// left completely untouched, including a fully ALL-CAPS word -- there is no
// reliable way to distinguish a real acronym from a shouty title by casing
// pattern alone, so this is an accepted trade-off rather than a bug.
func FormatTitle(title string, hyphenExceptions []string) string {
	title = strings.ReplaceAll(title, "_", " ")
	title = applyHyphenExceptions(title, hyphenExceptions)

	words := strings.Fields(title)
	for i, w := range words {
		if hasInternalUpper(w) {
			continue
		}
		if i != 0 && i != len(words)-1 {
			if key, ok := wordCore(w); ok && smallWords[key] {
				words[i] = strings.ToLower(w)
				continue
			}
		}
		words[i] = capitalizeFirst(w)
	}
	return strings.Join(words, " ")
}

// applyHyphenExceptions replaces each hyphen-joined run of word characters
// in s with its canonical casing from hyphenExceptions if it matches one
// (case-insensitively), otherwise replaces the run's hyphens with spaces.
func applyHyphenExceptions(s string, hyphenExceptions []string) string {
	canonicalByLower := make(map[string]string, len(hyphenExceptions))
	for _, e := range hyphenExceptions {
		canonicalByLower[strings.ToLower(e)] = e
	}
	return hyphenRunRe.ReplaceAllStringFunc(s, func(run string) string {
		if canonical, ok := canonicalByLower[strings.ToLower(run)]; ok {
			return canonical
		}
		return strings.ReplaceAll(run, "-", " ")
	})
}

// hasInternalUpper reports whether w has an uppercase letter anywhere after
// its first letter.
func hasInternalUpper(w string) bool {
	seenFirstLetter := false
	for _, r := range w {
		if !unicode.IsLetter(r) {
			continue
		}
		if !seenFirstLetter {
			seenFirstLetter = true
			continue
		}
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

// wordCore extracts w's contiguous run of letters, lowercased, for
// small-word lookup -- e.g. "(the)" -> "the", true.
func wordCore(w string) (string, bool) {
	m := wordCoreRe.FindStringSubmatch(w)
	if m == nil {
		return "", false
	}
	return strings.ToLower(m[1]), true
}

// capitalizeFirst uppercases w's first letter and lowercases every letter
// after it, leaving non-letters untouched.
func capitalizeFirst(w string) string {
	runes := []rune(w)
	seenFirstLetter := false
	for i, r := range runes {
		if !unicode.IsLetter(r) {
			continue
		}
		if !seenFirstLetter {
			runes[i] = unicode.ToUpper(r)
			seenFirstLetter = true
			continue
		}
		runes[i] = unicode.ToLower(r)
	}
	return string(runes)
}
