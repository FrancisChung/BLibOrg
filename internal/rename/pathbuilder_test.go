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
			Fallbacks:      config.Fallbacks{Year: "Unknown", Author: "Unknown Author"},
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

func TestBuildPath_UsesFallbacksForUnresolvedFields(t *testing.T) {
	b := &book.Book{
		SourcePath:  "/inbox/mystery.epub",
		Title:       book.Field{Value: "Mystery Book"},
		Author:      book.Field{Value: ""},
		Year:        book.Field{Value: ""},
		Category:    "Uncategorized",
		Subcategory: "",
	}
	BuildPath(b, testConfig("/library"))

	if !strings.Contains(b.DestPath, "Unknown Author") || !strings.Contains(b.DestPath, "Unknown") {
		t.Errorf("DestPath = %q, want it to contain the configured fallback text", b.DestPath)
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
