package appapi

import (
	"testing"

	"github.com/FrancisChung/book-organiser/internal/book"
)

func TestBookToView_FullyResolvedBook(t *testing.T) {
	b := &book.Book{
		SourcePath: "/inbox/foundation.epub",
		Format:     "epub",
		SizeBytes:  12345,
		Subject:    "Sci-Fi",
		Title:      book.Field{Value: "Foundation", Source: book.SourceMetadata},
		Author:     book.Field{Value: "Isaac Asimov", Source: book.SourceMetadata},
		Year:       book.Field{Value: "1951", Source: book.SourceMetadata},
		Category:    "Fiction",
		Subcategory: "Sci-Fi",
		DestPath:    "/library/Fiction/Sci-Fi/Foundation (1951) - Isaac Asimov.epub",
	}

	v := bookToView(b)

	if v.ID != "/inbox/foundation.epub" || v.SourcePath != "/inbox/foundation.epub" {
		t.Errorf("ID/SourcePath = %q/%q, want SourcePath in both", v.ID, v.SourcePath)
	}
	if v.OldFilename != "foundation.epub" {
		t.Errorf("OldFilename = %q, want foundation.epub", v.OldFilename)
	}
	if v.Title != (Field{Value: "Foundation", Source: "Metadata"}) {
		t.Errorf("Title = %+v", v.Title)
	}
	if v.Author != (Field{Value: "Isaac Asimov", Source: "Metadata"}) {
		t.Errorf("Author = %+v", v.Author)
	}
	if v.Year != (Field{Value: "1951", Source: "Metadata"}) {
		t.Errorf("Year = %+v", v.Year)
	}
	if v.Status != "Metadata" {
		t.Errorf("Status = %q, want Metadata", v.Status)
	}
	if v.Category != "Fiction" || v.Subcategory != "Sci-Fi" {
		t.Errorf("Category/Subcategory = %q/%q", v.Category, v.Subcategory)
	}
	if v.DestPath != b.DestPath {
		t.Errorf("DestPath = %q, want %q", v.DestPath, b.DestPath)
	}
	if v.DuplicateStatus != "NotDuplicate" {
		t.Errorf("DuplicateStatus = %q, want NotDuplicate", v.DuplicateStatus)
	}
}

func TestBookToView_PartialStatusAndDuplicateFlag(t *testing.T) {
	b := &book.Book{
		SourcePath:       "/inbox/mystery.pdf",
		Format:           "pdf",
		Title:            book.Field{Value: "Some Title", Source: book.SourceHeuristic},
		Author:           book.Field{Value: "", Source: book.SourceUnresolved},
		Year:             book.Field{Value: "", Source: book.SourceUnresolved},
		DuplicateGroupID: "grp-1",
		DuplicateStatus:  book.LikelyDuplicate,
	}

	v := bookToView(b)

	if v.Status != "Partial" {
		t.Errorf("Status = %q, want Partial", v.Status)
	}
	if v.DuplicateStatus != "LikelyDuplicate" {
		t.Errorf("DuplicateStatus = %q, want LikelyDuplicate", v.DuplicateStatus)
	}
	if v.DuplicateGroupID != "grp-1" {
		t.Errorf("DuplicateGroupID = %q, want grp-1", v.DuplicateGroupID)
	}
}

func TestBookToView_UnresolvedTitle(t *testing.T) {
	b := &book.Book{
		SourcePath: "/inbox/unknown.pdf",
		Title:      book.Field{Value: "", Source: book.SourceUnresolved},
	}

	v := bookToView(b)

	if v.Status != "Unresolved" {
		t.Errorf("Status = %q, want Unresolved", v.Status)
	}
}

func TestBookToView_CarriesCategoryManual(t *testing.T) {
	b := &book.Book{
		SourcePath:     "/inbox/foundation.epub",
		Title:          book.Field{Value: "Foundation", Source: book.SourceMetadata},
		Category:       "Fiction",
		Subcategory:    "Sci-Fi",
		CategoryManual: true,
	}

	v := bookToView(b)

	if !v.CategoryManual {
		t.Error("CategoryManual = false, want true")
	}
	if v.Category != "Fiction" || v.Subcategory != "Sci-Fi" {
		t.Errorf("Category/Subcategory = %q/%q, want Fiction/Sci-Fi", v.Category, v.Subcategory)
	}
}
