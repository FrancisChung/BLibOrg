package textutil

import "testing"

func TestCleanField(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"no trailing punctuation", "Foundation", "Foundation"},
		{"trailing period", "Foundation.", "Foundation"},
		{"trailing semicolon", "Foundation;", "Foundation"},
		{"trailing period and whitespace", "Foundation. ", "Foundation"},
		{"trailing mixed run", "Foundation.; .", "Foundation"},
		{"internal period untouched", "J.R.R. Tolkien", "J.R.R. Tolkien"},
		{"empty string", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CleanField(tc.in)
			if got != tc.want {
				t.Errorf("CleanField(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeAuthorSeparators(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"no semicolons", "Isaac Asimov", "Isaac Asimov"},
		{"two authors", "Bruce Eckel;Svetlana Isakova", "Bruce Eckel, Svetlana Isakova"},
		{"two authors with spacing", "Bruce Eckel ; Svetlana Isakova", "Bruce Eckel, Svetlana Isakova"},
		{"three authors", "A;B;C", "A, B, C"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeAuthorSeparators(tc.in)
			if got != tc.want {
				t.Errorf("NormalizeAuthorSeparators(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
