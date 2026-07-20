package textutil

import "testing"

func TestFormatTitle(t *testing.T) {
	cases := []struct {
		name       string
		title      string
		exceptions []string
		want       string
	}{
		{
			name:  "underscores become spaces",
			title: "Clean_Code_Handbook",
			want:  "Clean Code Handbook",
		},
		{
			name:  "hyphens become spaces when not an exception",
			title: "software-architecture-for-developers",
			want:  "Software Architecture for Developers",
		},
		{
			name:       "hyphen exception is preserved with its canonical casing",
			title:      "high-performance systems",
			exceptions: []string{"High-Performance"},
			want:       "High-Performance Systems",
		},
		{
			name:       "hyphen exception match is case-insensitive",
			title:      "HIGH-PERFORMANCE systems",
			exceptions: []string{"High-Performance"},
			want:       "High-Performance Systems",
		},
		{
			name:  "small words lowercased mid-title but capitalized at the edges",
			title: "a guide to the mountain of fire",
			want:  "A Guide to the Mountain of Fire",
		},
		{
			name:  "a mixed-case word is preserved untouched",
			title: "the iOS developer guide",
			want:  "The iOS Developer Guide",
		},
		{
			name:  "an ALL-CAPS word is preserved untouched",
			title: "API DESIGN PATTERNS",
			want:  "API DESIGN PATTERNS",
		},
		{
			name:  "apostrophe-s stays lowercase after capitalizing the word",
			title: "the consultant's guide",
			want:  "The Consultant's Guide",
		},
		{
			name:  "empty title stays empty",
			title: "",
			want:  "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatTitle(tc.title, tc.exceptions)
			if got != tc.want {
				t.Errorf("FormatTitle(%q, %v) = %q, want %q", tc.title, tc.exceptions, got, tc.want)
			}
		})
	}
}
