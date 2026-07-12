package rename

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/FrancisChung/book-organiser/internal/book"
	"github.com/FrancisChung/book-organiser/internal/config"
)

func testConfig(libraryFolder string) config.Config {
	return config.Config{
		General: config.General{
			LibraryFolder:  libraryFolder,
			FilenameFormat: "{title} ({year}) - {author}",
		},
	}
}

func TestBuildPath_NormalRender(t *testing.T) {
	b := &book.Book{
		SourcePath:  "/inbox/foundation.epub",
		Title:       book.Field{Value: "Foundation"},
		Author:      book.Field{Value: "Isaac Asimov"},
		Year:        book.Field{Value: "1951"},
		Category:    "Fiction",
		Subcategory: "Sci-Fi",
	}
	BuildPath(b, testConfig("/library"))

	want := filepath.Join("/library", "Fiction", "Sci-Fi", "Foundation (1951) - Isaac Asimov.epub")
	if b.DestPath != want {
		t.Errorf("DestPath = %q, want %q", b.DestPath, want)
	}
}

func TestBuildPath_OmitsUnresolvedAuthorAndYearFromFilename(t *testing.T) {
	b := &book.Book{
		SourcePath:  "/inbox/mystery.epub",
		Title:       book.Field{Value: "Mystery Book"},
		Author:      book.Field{Value: ""},
		Year:        book.Field{Value: ""},
		Category:    "Uncategorized",
		Subcategory: "",
	}
	BuildPath(b, testConfig("/library"))

	want := filepath.Join("/library", "Uncategorized", "Mystery Book.epub")
	if b.DestPath != want {
		t.Errorf("DestPath = %q, want %q (no fallback placeholder text, no stray parens/dashes)", b.DestPath, want)
	}
}

func TestBuildPath_OmitsUnresolvedAuthorOnlyKeepsYear(t *testing.T) {
	b := &book.Book{
		SourcePath: "/inbox/mystery.epub",
		Title:      book.Field{Value: "Mystery Book"},
		Author:     book.Field{Value: ""},
		Year:       book.Field{Value: "2024"},
		Category:   "Uncategorized",
	}
	BuildPath(b, testConfig("/library"))

	want := filepath.Join("/library", "Uncategorized", "Mystery Book (2024).epub")
	if b.DestPath != want {
		t.Errorf("DestPath = %q, want %q", b.DestPath, want)
	}
}

func TestBuildPath_OmitsUnresolvedYearOnlyKeepsAuthor(t *testing.T) {
	b := &book.Book{
		SourcePath: "/inbox/mystery.epub",
		Title:      book.Field{Value: "Mystery Book"},
		Author:     book.Field{Value: "Agatha Christie"},
		Year:       book.Field{Value: ""},
		Category:   "Uncategorized",
	}
	BuildPath(b, testConfig("/library"))

	want := filepath.Join("/library", "Uncategorized", "Mystery Book - Agatha Christie.epub")
	if b.DestPath != want {
		t.Errorf("DestPath = %q, want %q", b.DestPath, want)
	}
}

func TestBuildPath_PreservesLeadingHyphenInFullyResolvedTitle(t *testing.T) {
	b := &book.Book{
		SourcePath: "/inbox/dash.epub",
		Title:      book.Field{Value: "-Leading Dash Title"},
		Author:     book.Field{Value: "Jane Doe"},
		Year:       book.Field{Value: "2020"},
		Category:   "Uncategorized",
	}
	BuildPath(b, testConfig("/library"))

	want := filepath.Join("/library", "Uncategorized", "-Leading Dash Title (2020) - Jane Doe.epub")
	if b.DestPath != want {
		t.Errorf("DestPath = %q, want %q (title's own leading hyphen must survive when nothing was omitted)", b.DestPath, want)
	}
}

func TestBuildPath_PreservesTrailingHyphenInFullyResolvedAuthor(t *testing.T) {
	b := &book.Book{
		SourcePath: "/inbox/dash.epub",
		Title:      book.Field{Value: "Book Title"},
		Author:     book.Field{Value: "Jane Doe-"},
		Year:       book.Field{Value: "2020"},
		Category:   "Uncategorized",
	}
	BuildPath(b, testConfig("/library"))

	want := filepath.Join("/library", "Uncategorized", "Book Title (2020) - Jane Doe-.epub")
	if b.DestPath != want {
		t.Errorf("DestPath = %q, want %q (author's own trailing hyphen must survive when nothing was omitted)", b.DestPath, want)
	}
}

func TestBuildPath_SanitizesIllegalCharsAndReservedNames(t *testing.T) {
	b := &book.Book{
		SourcePath: "/inbox/weird.epub",
		Title:      book.Field{Value: `CON: A Book? "Title" <Test>`},
		Author:     book.Field{Value: "Someone"},
		Year:       book.Field{Value: "2024"},
		Category:   "Uncategorized",
	}
	BuildPath(b, testConfig("/library"))

	base := filepath.Base(b.DestPath)
	for _, illegal := range []string{"<", ">", ":", `"`, "?"} {
		if strings.Contains(base, illegal) {
			t.Errorf("DestPath base %q still contains illegal character %q", base, illegal)
		}
	}
}

func TestBuildPath_DropsAuthorBeforeTruncatingTitle(t *testing.T) {
	longTitle := strings.Repeat("VeryLongTitleWord ", 20) // ~360 chars
	b := &book.Book{
		SourcePath: "/inbox/long.epub",
		Title:      book.Field{Value: longTitle},
		Author:     book.Field{Value: "Some Author Name That Adds Length"},
		Year:       book.Field{Value: "2024"},
		Category:   "Uncategorized",
	}
	// A long library folder path pushes the total over the internal budget
	// even before considering the long title, forcing truncation.
	BuildPath(b, testConfig(filepath.Join("/", strings.Repeat("x", 100))))

	if strings.Contains(b.DestPath, "Some Author Name") {
		t.Errorf("expected author to be dropped from an over-length path, got %q", b.DestPath)
	}
	if len(b.DestPath) > 260 {
		t.Errorf("DestPath length %d still exceeds a safe Windows path budget: %q", len(b.DestPath), b.DestPath)
	}
}

// TestBuildPath_TruncatesTitleToEmptyForVeryLongLibraryFolder is a regression
// test for a bug in an earlier version of the truncation loop: it stopped
// once the title shrank to <= truncateStep characters instead of continuing
// down to "", so a sufficiently long library folder path could leave
// DestPath well over the 260-char safe budget with an untouched title tail
// still attached. With a 230-char library folder component, the folder
// alone already pushes the path over maxPathLen (240), so the fixed loop
// must drive the title all the way to "" (collapsing the filename to just
// "(2024).epub") to reach its shortest achievable result. The old
// stop-at-truncateStep loop would have left 10 title characters in place
// here, producing a name of "VeryLongTi (2024).epub" (22 chars) instead of
// "(2024).epub" (11 chars) -- 11 bytes that push this exact case's DestPath
// from 257 chars (within budget) to 268 chars (over budget).
func TestBuildPath_TruncatesTitleToEmptyForVeryLongLibraryFolder(t *testing.T) {
	longTitle := strings.Repeat("VeryLongTitleWord ", 20) // 360 chars, a multiple of truncateStep
	b := &book.Book{
		SourcePath: "/inbox/long.epub",
		Title:      book.Field{Value: longTitle},
		Author:     book.Field{Value: "Some Author Name That Adds Length"},
		Year:       book.Field{Value: "2024"},
		Category:   "Uncategorized",
	}
	// A 230-char library folder component makes the library/category
	// portion alone exceed maxPathLen (240), so no amount of title
	// truncation can bring the path fully back under maxPathLen -- but the
	// fix must still drive the title to "" to get as close as possible.
	BuildPath(b, testConfig(filepath.Join("/", strings.Repeat("x", 230))))

	wantBase := "(2024).epub"
	if gotBase := filepath.Base(b.DestPath); gotBase != wantBase {
		t.Errorf("DestPath base = %q, want %q (title should be fully truncated, not left with a partial tail)", gotBase, wantBase)
	}
	if len(b.DestPath) > 260 {
		t.Errorf("DestPath length %d still exceeds a safe Windows path budget: %q", len(b.DestPath), b.DestPath)
	}
}
