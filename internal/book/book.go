package book

type Source int

const (
	SourceUnresolved Source = iota
	SourceMetadata
	SourceHeuristic
	SourceManual
)

func (s Source) String() string {
	switch s {
	case SourceMetadata:
		return "Metadata"
	case SourceHeuristic:
		return "Heuristic"
	case SourceManual:
		return "Edited"
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

	Subject     string
	Category    string
	Subcategory string
	DestPath    string

	DuplicateGroupID string
	DuplicateStatus  DuplicateStatus
}

// Status returns the row-level status shown in View 1, with precedence
// Unresolved > Edited (Manual) > Heuristic > Metadata: any single Unresolved
// required field makes the whole row Unresolved (excluded from Apply),
// regardless of how the other fields were resolved.
func (b Book) Status() Source {
	fields := []Field{b.Title, b.Author, b.Year}

	for _, f := range fields {
		if f.Source == SourceUnresolved {
			return SourceUnresolved
		}
	}
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
