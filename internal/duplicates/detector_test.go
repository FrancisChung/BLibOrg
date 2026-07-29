package duplicates

import (
	"testing"

	"github.com/FrancisChung/BLibOrg/internal/book"
)

func TestDetect_LikelyDuplicateSameFormatAndSize(t *testing.T) {
	a := &book.Book{Title: book.Field{Value: "Foundation"}, Author: book.Field{Value: "Isaac Asimov"}, Format: "epub", SizeBytes: 1_000_000}
	b := &book.Book{Title: book.Field{Value: "foundation"}, Author: book.Field{Value: "  isaac asimov "}, Format: "epub", SizeBytes: 1_005_000}
	books := []*book.Book{a, b}

	Detect(books)

	if a.DuplicateGroupID == "" || a.DuplicateGroupID != b.DuplicateGroupID {
		t.Fatalf("expected same non-empty group ID, got %q and %q", a.DuplicateGroupID, b.DuplicateGroupID)
	}
	if a.DuplicateStatus != book.LikelyDuplicate || b.DuplicateStatus != book.LikelyDuplicate {
		t.Errorf("DuplicateStatus = %v / %v, want LikelyDuplicate for both", a.DuplicateStatus, b.DuplicateStatus)
	}
}

func TestDetect_PossibleDuplicateDifferentFormat(t *testing.T) {
	a := &book.Book{Title: book.Field{Value: "Foundation"}, Author: book.Field{Value: "Isaac Asimov"}, Format: "epub", SizeBytes: 1_000_000}
	b := &book.Book{Title: book.Field{Value: "Foundation"}, Author: book.Field{Value: "Isaac Asimov"}, Format: "pdf", SizeBytes: 5_000_000}
	books := []*book.Book{a, b}

	Detect(books)

	if a.DuplicateGroupID != b.DuplicateGroupID || a.DuplicateGroupID == "" {
		t.Fatalf("expected same non-empty group ID")
	}
	if a.DuplicateStatus != book.PossibleDuplicate {
		t.Errorf("DuplicateStatus = %v, want PossibleDuplicate", a.DuplicateStatus)
	}
}

func TestDetect_NoDuplicateWhenTitleAuthorDiffer(t *testing.T) {
	a := &book.Book{Title: book.Field{Value: "Foundation"}, Author: book.Field{Value: "Isaac Asimov"}, Format: "epub", SizeBytes: 1_000_000}
	b := &book.Book{Title: book.Field{Value: "Dune"}, Author: book.Field{Value: "Frank Herbert"}, Format: "epub", SizeBytes: 1_000_000}
	books := []*book.Book{a, b}

	Detect(books)

	if a.DuplicateGroupID != "" || b.DuplicateGroupID != "" {
		t.Errorf("expected no group assigned, got %q and %q", a.DuplicateGroupID, b.DuplicateGroupID)
	}
}

func TestDetect_PerBookStatusInGroupOfThree(t *testing.T) {
	a := &book.Book{Title: book.Field{Value: "Foundation"}, Author: book.Field{Value: "Isaac Asimov"}, Format: "epub", SizeBytes: 1_000_000}
	b := &book.Book{Title: book.Field{Value: "Foundation"}, Author: book.Field{Value: "Isaac Asimov"}, Format: "epub", SizeBytes: 1_005_000}
	c := &book.Book{Title: book.Field{Value: "Foundation"}, Author: book.Field{Value: "Isaac Asimov"}, Format: "pdf", SizeBytes: 9_000_000}
	books := []*book.Book{a, b, c}

	Detect(books)

	if a.DuplicateGroupID == "" || a.DuplicateGroupID != b.DuplicateGroupID || a.DuplicateGroupID != c.DuplicateGroupID {
		t.Fatalf("expected all three to share the same non-empty group ID, got %q, %q, %q", a.DuplicateGroupID, b.DuplicateGroupID, c.DuplicateGroupID)
	}
	if a.DuplicateStatus != book.LikelyDuplicate {
		t.Errorf("a.DuplicateStatus = %v, want LikelyDuplicate", a.DuplicateStatus)
	}
	if b.DuplicateStatus != book.LikelyDuplicate {
		t.Errorf("b.DuplicateStatus = %v, want LikelyDuplicate", b.DuplicateStatus)
	}
	if c.DuplicateStatus != book.PossibleDuplicate {
		t.Errorf("c.DuplicateStatus = %v, want PossibleDuplicate (matches neither format nor size of any group member)", c.DuplicateStatus)
	}
}

func TestNormalize_PreservesAccentedLetters(t *testing.T) {
	got := normalize("  GARCÍA Márquez  ")
	want := "garcía márquez"
	if got != want {
		t.Errorf("normalize(%q) = %q, want %q (accented letters must be preserved, not treated as separators)", "  GARCÍA Márquez  ", got, want)
	}
}

func TestNormalize_UnicodeAccentedLetters(t *testing.T) {
	a := &book.Book{Title: book.Field{Value: "One Hundred Years of Solitude"}, Author: book.Field{Value: "García Márquez"}, Format: "epub", SizeBytes: 1_000_000}
	b := &book.Book{Title: book.Field{Value: "One Hundred Years of Solitude"}, Author: book.Field{Value: "  GARCÍA MÁRQUEZ "}, Format: "epub", SizeBytes: 1_000_500}
	books := []*book.Book{a, b}

	Detect(books)

	if a.DuplicateGroupID == "" || a.DuplicateGroupID != b.DuplicateGroupID {
		t.Fatalf("expected same non-empty group ID for differently-cased/spaced accented author, got %q and %q", a.DuplicateGroupID, b.DuplicateGroupID)
	}
	if a.DuplicateStatus != book.LikelyDuplicate || b.DuplicateStatus != book.LikelyDuplicate {
		t.Errorf("DuplicateStatus = %v / %v, want LikelyDuplicate for both", a.DuplicateStatus, b.DuplicateStatus)
	}
}

func TestDetect_SkipsBooksWithNoTitleOrAuthor(t *testing.T) {
	a := &book.Book{Title: book.Field{Value: ""}, Author: book.Field{Value: ""}, Format: "epub", SizeBytes: 1_000_000}
	b := &book.Book{Title: book.Field{Value: ""}, Author: book.Field{Value: ""}, Format: "epub", SizeBytes: 1_000_000}
	books := []*book.Book{a, b}

	Detect(books)

	if a.DuplicateGroupID != "" || b.DuplicateGroupID != "" {
		t.Errorf("expected books with no title/author to never be grouped, got %q and %q", a.DuplicateGroupID, b.DuplicateGroupID)
	}
}
