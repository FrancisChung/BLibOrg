package textutil

import "testing"

func TestExtractYear(t *testing.T) {
	tests := []struct {
		in       string
		wantYear string
		wantOK   bool
	}{
		{"1951-01-01", "1951", true},
		{"(2024, O'Reilly Media, Inc.)", "2024", true},
		{"no year here", "", false},
		{"a 1999 release", "1999", true},
		{"", "", false},
	}
	for _, tt := range tests {
		year, ok := ExtractYear(tt.in)
		if year != tt.wantYear || ok != tt.wantOK {
			t.Errorf("ExtractYear(%q) = (%q, %v), want (%q, %v)", tt.in, year, ok, tt.wantYear, tt.wantOK)
		}
	}
}
