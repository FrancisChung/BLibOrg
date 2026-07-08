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
			name: "any unresolved field wins regardless of others",
			book: Book{
				Title:  Field{"T", SourceManual},
				Author: Field{"", SourceUnresolved},
				Year:   Field{"2024", SourceMetadata},
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
