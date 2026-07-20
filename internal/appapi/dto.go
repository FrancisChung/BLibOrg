// Package appapi adapts the existing book-organiser backend packages
// (pipeline, categorizer, rename, operations) into a small set of
// JSON-friendly methods a UI layer can call directly. It contains no
// business logic of its own -- only DTO conversion and delegation.
package appapi

import (
	"path/filepath"

	"github.com/FrancisChung/book-organiser/internal/book"
)

// Field mirrors book.Field with Source rendered as its display string
// (e.g. "Metadata", "Heuristic", "Edited", "Unresolved") so the frontend
// never needs to know about the underlying Go enum.
type Field struct {
	Value  string `json:"value"`
	Source string `json:"source"`
}

// BookView is the JSON-serializable view of a book.Book sent to and
// received from the frontend.
type BookView struct {
	ID               string `json:"id"`
	SourcePath       string `json:"sourcePath"`
	OldFilename      string `json:"oldFilename"`
	Format           string `json:"format"`
	SizeBytes        int64  `json:"sizeBytes"`
	Subject          string `json:"subject"`
	Title            Field  `json:"title"`
	Author           Field  `json:"author"`
	Year             Field  `json:"year"`
	Status           string `json:"status"`
	Category         string `json:"category"`
	Subcategory      string `json:"subcategory"`
	CategoryWarning  string `json:"categoryWarning"`
	CategoryManual   bool   `json:"categoryManual"`
	DestPath         string `json:"destPath"`
	DuplicateStatus  string `json:"duplicateStatus"`
	DuplicateGroupID string `json:"duplicateGroupId"`
}

func fieldToView(f book.Field) Field {
	return Field{Value: f.Value, Source: f.Source.String()}
}

func duplicateStatusToView(s book.DuplicateStatus) string {
	switch s {
	case book.LikelyDuplicate:
		return "LikelyDuplicate"
	case book.PossibleDuplicate:
		return "PossibleDuplicate"
	default:
		return "NotDuplicate"
	}
}

// bookToView converts a fully-processed book.Book (after Categorize and
// BuildPath have run) into its JSON view.
func bookToView(b *book.Book) BookView {
	return BookView{
		ID:               b.SourcePath,
		SourcePath:       b.SourcePath,
		OldFilename:      filepath.Base(b.SourcePath),
		Format:           b.Format,
		SizeBytes:        b.SizeBytes,
		Subject:          b.Subject,
		Title:            fieldToView(b.Title),
		Author:           fieldToView(b.Author),
		Year:             fieldToView(b.Year),
		Status:           b.Status().String(),
		Category:         b.Category,
		Subcategory:      b.Subcategory,
		CategoryWarning:  b.CategoryWarning,
		CategoryManual:   b.CategoryManual,
		DestPath:         b.DestPath,
		DuplicateStatus:  duplicateStatusToView(b.DuplicateStatus),
		DuplicateGroupID: b.DuplicateGroupID,
	}
}

func sourceFromView(s string) book.Source {
	switch s {
	case "Metadata":
		return book.SourceMetadata
	case "Heuristic":
		return book.SourceHeuristic
	case "Edited":
		return book.SourceManual
	default:
		return book.SourceUnresolved
	}
}

func fieldFromView(f Field) book.Field {
	return book.Field{Value: f.Value, Source: sourceFromView(f.Source)}
}

func duplicateStatusFromView(s string) book.DuplicateStatus {
	switch s {
	case "LikelyDuplicate":
		return book.LikelyDuplicate
	case "PossibleDuplicate":
		return book.PossibleDuplicate
	default:
		return book.NotDuplicate
	}
}

// viewToBook converts a BookView back into a book.Book carrying only the
// fields that are genuine inputs to Categorize/BuildPath/Status. DestPath
// and Status itself are pure outputs, never carried over. Category and
// Subcategory ARE carried over (as of CategoryManual support) so a manual
// destination pick survives into Categorize, which itself immediately
// returns without touching them when CategoryManual is true; when it's
// false these values are just overwritten by Categorize as before.
func viewToBook(v BookView) *book.Book {
	return &book.Book{
		SourcePath:       v.SourcePath,
		Format:           v.Format,
		SizeBytes:        v.SizeBytes,
		Subject:          v.Subject,
		Title:            fieldFromView(v.Title),
		Author:           fieldFromView(v.Author),
		Year:             fieldFromView(v.Year),
		Category:         v.Category,
		Subcategory:      v.Subcategory,
		CategoryManual:   v.CategoryManual,
		DuplicateGroupID: v.DuplicateGroupID,
		DuplicateStatus:  duplicateStatusFromView(v.DuplicateStatus),
	}
}
