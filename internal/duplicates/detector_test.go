package duplicates

import (
	"testing"

	"github.com/FrancisChung/book-organiser/internal/book"
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

func TestDetect_SkipsBooksWithNoTitleOrAuthor(t *testing.T) {
	a := &book.Book{Title: book.Field{Value: ""}, Author: book.Field{Value: ""}, Format: "epub", SizeBytes: 1_000_000}
	b := &book.Book{Title: book.Field{Value: ""}, Author: book.Field{Value: ""}, Format: "epub", SizeBytes: 1_000_000}
	books := []*book.Book{a, b}

	Detect(books)

	if a.DuplicateGroupID != "" || b.DuplicateGroupID != "" {
		t.Errorf("expected books with no title/author to never be grouped, got %q and %q", a.DuplicateGroupID, b.DuplicateGroupID)
	}
}
