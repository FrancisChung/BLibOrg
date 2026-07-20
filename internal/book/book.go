package book

type Source int

const (
	SourceUnresolved Source = iota
	SourceMetadata
	SourceHeuristic
	SourceManual
	// SourcePartial is a row-level-only value: Book.Status() returns it when
	// Title is resolved but Author and/or Year are not. It is never a valid
	// value for an individual Field.Source -- a single field is either
	// resolved (Metadata/Heuristic/Manual) or it isn't (Unresolved); only the
	// aggregate row can be "partially" complete.
	SourcePartial
)

func (s Source) String() string {
	switch s {
	case SourceMetadata:
		return "Metadata"
	case SourceHeuristic:
		return "Heuristic"
	case SourceManual:
		return "Edited"
	case SourcePartial:
		return "Partial"
	default:
		return "Unresolved"
	}
}

type Field struct {
	Value  string
	Source Source
}

type DuplicateStatus int

const (
	NotDuplicate DuplicateStatus = iota
	PossibleDuplicate
	LikelyDuplicate
)

type Book struct {
	SourcePath string
	Format     string
	SizeBytes  int64

	Title  Field
	Author Field
	Year   Field

	Subject         string
	Category        string
	Subcategory     string
	CategoryManual  bool
	CategoryWarning string
	DestPath        string

	DuplicateGroupID string
	DuplicateStatus  DuplicateStatus
}

// Status returns the row-level status shown in View 1, with precedence
// Unresolved > Partial > Edited (Manual) > Heuristic > Metadata. Title is
// the one field Apply cannot proceed without, so an unresolved Title makes
// the whole row Unresolved regardless of Author/Year. If Title is resolved
// but Author and/or Year are not, the row is Partial -- still eligible for
// Apply (the rendered path simply omits the missing field's segment rather
// than showing placeholder text), just flagged as incomplete. Only once
// all three fields are resolved does the status reflect their
// Edited/Heuristic/Metadata provenance.
func (b Book) Status() Source {
	if b.Title.Source == SourceUnresolved {
		return SourceUnresolved
	}
	if b.Author.Source == SourceUnresolved || b.Year.Source == SourceUnresolved {
		return SourcePartial
	}
	if b.CategoryManual {
		return SourceManual
	}

	fields := []Field{b.Title, b.Author, b.Year}
	for _, f := range fields {
		if f.Source == SourceManual {
			return SourceManual
		}
	}
	for _, f := range fields {
		if f.Source == SourceHeuristic {
			return SourceHeuristic
		}
	}
	return SourceMetadata
}
