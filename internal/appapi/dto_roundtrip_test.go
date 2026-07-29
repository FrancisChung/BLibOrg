package appapi

import (
	"testing"

	"github.com/FrancisChung/BLibOrg/internal/book"
)

func TestViewToBook_RoundTripsInputFields(t *testing.T) {
	v := BookView{
		SourcePath: "/inbox/foundation.epub",
		Format:     "epub",
		SizeBytes:  12345,
		Subject:    "Sci-Fi",
		Title:      Field{Value: "Foundation", Source: "Edited"},
		Author:     Field{Value: "Isaac Asimov", Source: "Metadata"},
		Year:       Field{Value: "1951", Source: "Heuristic"},
		DuplicateStatus:  "PossibleDuplicate",
		DuplicateGroupID: "grp-9",
	}

	b := viewToBook(v)

	if b.SourcePath != v.SourcePath || b.Format != v.Format || b.SizeBytes != v.SizeBytes || b.Subject != v.Subject {
		t.Errorf("passthrough fields mismatch: %+v", b)
	}
	if b.Title != (book.Field{Value: "Foundation", Source: book.SourceManual}) {
		t.Errorf("Title = %+v", b.Title)
	}
	if b.Author != (book.Field{Value: "Isaac Asimov", Source: book.SourceMetadata}) {
		t.Errorf("Author = %+v", b.Author)
	}
	if b.Year != (book.Field{Value: "1951", Source: book.SourceHeuristic}) {
		t.Errorf("Year = %+v", b.Year)
	}
	if b.DuplicateStatus != book.PossibleDuplicate || b.DuplicateGroupID != "grp-9" {
		t.Errorf("duplicate fields mismatch: %+v", b)
	}
}

func TestViewToBook_UnknownSourceStringBecomesUnresolved(t *testing.T) {
	v := BookView{Title: Field{Value: "x", Source: "not-a-real-source"}}
	b := viewToBook(v)
	if b.Title.Source != book.SourceUnresolved {
		t.Errorf("Title.Source = %v, want SourceUnresolved for an unrecognized source string", b.Title.Source)
	}
}

func TestViewToBook_RoundTripsCategoryFields(t *testing.T) {
	v := BookView{
		SourcePath:     "/inbox/foundation.epub",
		Category:       "Fiction",
		Subcategory:    "Sci-Fi",
		CategoryManual: true,
	}

	b := viewToBook(v)

	if b.Category != "Fiction" || b.Subcategory != "Sci-Fi" || !b.CategoryManual {
		t.Errorf("Category/Subcategory/CategoryManual = %q/%q/%v, want Fiction/Sci-Fi/true", b.Category, b.Subcategory, b.CategoryManual)
	}
}
