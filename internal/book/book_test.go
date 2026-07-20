package book

import "testing"

func TestSourceString(t *testing.T) {
	tests := []struct {
		s    Source
		want string
	}{
		{SourceMetadata, "Metadata"},
		{SourceHeuristic, "Heuristic"},
		{SourceManual, "Edited"},
		{SourceUnresolved, "Unresolved"},
		{SourcePartial, "Partial"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("Source(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}

func TestBookStatus(t *testing.T) {
	tests := []struct {
		name   string
		book   Book
		want   Source
	}{
		{
			name: "all metadata",
			book: Book{
				Title:  Field{"T", SourceMetadata},
				Author: Field{"A", SourceMetadata},
				Year:   Field{"2024", SourceMetadata},
			},
			want: SourceMetadata,
		},
		{
			name: "one heuristic field",
			book: Book{
				Title:  Field{"T", SourceMetadata},
				Author: Field{"A", SourceHeuristic},
				Year:   Field{"2024", SourceMetadata},
			},
			want: SourceHeuristic,
		},
		{
			name: "manually edited beats heuristic",
			book: Book{
				Title:  Field{"T", SourceManual},
				Author: Field{"A", SourceHeuristic},
				Year:   Field{"2024", SourceMetadata},
			},
			want: SourceManual,
		},
		{
			name: "title unresolved wins regardless of others -- Apply cannot proceed without a title",
			book: Book{
				Title:  Field{"", SourceUnresolved},
				Author: Field{"A", SourceMetadata},
				Year:   Field{"2024", SourceMetadata},
			},
			want: SourceUnresolved,
		},
		{
			name: "title resolved but author unresolved -- Partial, not Unresolved",
			book: Book{
				Title:  Field{"T", SourceHeuristic},
				Author: Field{"", SourceUnresolved},
				Year:   Field{"2024", SourceMetadata},
			},
			want: SourcePartial,
		},
		{
			name: "title resolved but year unresolved -- Partial, not Unresolved",
			book: Book{
				Title:  Field{"T", SourceMetadata},
				Author: Field{"A", SourceMetadata},
				Year:   Field{"", SourceUnresolved},
			},
			want: SourcePartial,
		},
		{
			name: "title resolved, both author and year unresolved -- still just Partial",
			book: Book{
				Title:  Field{"T", SourceHeuristic},
				Author: Field{"", SourceUnresolved},
				Year:   Field{"", SourceUnresolved},
			},
			want: SourcePartial,
		},
		{
			name: "title manually edited but author unresolved -- Partial beats Edited",
			book: Book{
				Title:  Field{"T", SourceManual},
				Author: Field{"", SourceUnresolved},
				Year:   Field{"2024", SourceMetadata},
			},
			want: SourcePartial,
		},
		{
			name: "manual category pick reports Edited even when title/author/year are all Metadata",
			book: Book{
				Title:          Field{"T", SourceMetadata},
				Author:         Field{"A", SourceMetadata},
				Year:           Field{"2024", SourceMetadata},
				CategoryManual: true,
			},
			want: SourceManual,
		},
		{
			name: "manual category pick does not override an unresolved title",
			book: Book{
				Title:          Field{"", SourceUnresolved},
				Author:         Field{"A", SourceMetadata},
				Year:           Field{"2024", SourceMetadata},
				CategoryManual: true,
			},
			want: SourceUnresolved,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.book.Status(); got != tt.want {
				t.Errorf("Status() = %v, want %v", got, tt.want)
			}
		})
	}
}
