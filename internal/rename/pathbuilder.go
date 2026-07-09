// Package rename renders cfg.General.FilenameFormat into a sanitized,
// length-budgeted destination path for a book.
package rename

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/FrancisChung/book-organiser/internal/book"
	"github.com/FrancisChung/book-organiser/internal/config"
)

var illegalCharsRe = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)
var trailingSepRe = regexp.MustCompile(`[\s\-\x{2013}\x{2014}]+$`)

var reservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// maxPathLen is a conservative budget for the full destination path
// (library folder + category + subcategory + filename), kept safely under
// Windows' 260-char MAX_PATH regardless of host OS, per the plan's Global
// Constraints on cross-platform path safety.
const maxPathLen = 240

// truncateStep is the number of characters shaved off the title per
// truncation attempt while the rendered path still exceeds maxPathLen.
const truncateStep = 10

// sanitize strips characters illegal on NTFS, trims trailing spaces/dots
// (also disallowed on Windows), and dodges reserved device names -- applied
// unconditionally regardless of host OS. name is the rendered field
// combination (title/year/author) without the file extension; the caller
// appends the real extension afterward.
func sanitize(name string) string {
	name = illegalCharsRe.ReplaceAllString(name, "")
	name = strings.TrimRight(name, " .")
	if reservedNames[strings.ToUpper(name)] {
		name = "_" + name
	}
	if name == "" {
		name = "Untitled"
	}
	return name
}

func render(format, title, year, author string) string {
	r := strings.NewReplacer("{title}", title, "{year}", year, "{author}", author)
	return r.Replace(format)
}

func cleanupDanglingSeparators(s string) string {
	return strings.TrimSpace(trailingSepRe.ReplaceAllString(s, ""))
}

// BuildPath computes b.DestPath from cfg.General.FilenameFormat and
// b.Category/b.Subcategory. Unresolved year/author fields render using
// cfg.General.Fallbacks text for this preview only -- it does not change
// Field.Source, so Book.Status() still reports Unresolved for those rows.
// It mutates only b.DestPath.
//
// If the rendered path would exceed the safe length budget, the author is
// dropped from the filename first; only if that still isn't enough is the
// title itself progressively truncated, down to empty if necessary. If the
// library folder/category/subcategory portion alone already exceeds the
// budget, no amount of title truncation can bring the path back under
// budget; BuildPath still returns its best (shortest achievable) attempt
// rather than failing.
//
// Custom filename_format templates other than the default
// "{title} ({year}) - {author}" shape may leave a stray leading separator
// when the author is dropped; cleanupDanglingSeparators only strips
// trailing " - "/em-dash patterns, which covers the default and most
// conventional templates.
func BuildPath(b *book.Book, cfg config.Config) {
	title := b.Title.Value
	if title == "" {
		title = "Untitled"
	}
	year := b.Year.Value
	if year == "" {
		year = cfg.General.Fallbacks.Year
	}
	author := b.Author.Value
	if author == "" {
		author = cfg.General.Fallbacks.Author
	}

	ext := filepath.Ext(b.SourcePath)
	dir := filepath.Join(cfg.General.LibraryFolder, b.Category, b.Subcategory)

	build := func(t, a string) string {
		rendered := render(cfg.General.FilenameFormat, t, year, a)
		if a == "" {
			rendered = cleanupDanglingSeparators(rendered)
		}
		return sanitize(rendered) + ext
	}

	name := build(title, author)
	if len(filepath.Join(dir, name)) > maxPathLen {
		name = build(title, "")
	}
	// Progressively shrink the title (never below empty) until the path
	// fits the budget. Using min(truncateStep, len(title)) as the step,
	// rather than stopping once len(title) <= truncateStep, guarantees the
	// loop drives the title all the way to "" when needed instead of
	// leaving an over-budget path with an untouched short title tail.
	for len(filepath.Join(dir, name)) > maxPathLen && title != "" {
		step := truncateStep
		if step > len(title) {
			step = len(title)
		}
		title = title[:len(title)-step]
		name = build(title, "")
	}

	b.DestPath = filepath.Join(dir, name)
}
