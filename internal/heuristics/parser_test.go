package heuristics

import "testing"

func TestParse_RealWorldExamples(t *testing.T) {
	junkTags := []string{"OceanofPDF.com", "libgen.li", "libgen.rs", "z-lib.org"}

	tests := []struct {
		name string
		stem string
		want Result
	}{
		{
			name: "plus-delimited, no author or year present",
			stem: "Build+Your+API+with+Spring",
			want: Result{Title: "Build Your API with Spring", Author: "", Year: ""},
		},
		{
			name: "bracket noise with real year; author lost in braces (known limitation, needs manual fix in View 1)",
			stem: "Building Resilient Distributed Systems (for dagfhhhhh dfafaf){Sam Newman}(2024, O&_039_Reilly Media, Inc.){115667237} libgen.li",
			want: Result{Title: "Building Resilient Distributed Systems", Author: "", Year: "2024"},
		},
		{
			name: "site-tag prefix with title - author",
			stem: "_OceanofPDF.com_Dissecting_the_Dark_Web_-_Lindsay_Kaye",
			want: Result{Title: "Dissecting the Dark Web", Author: "Lindsay Kaye", Year: ""},
		},
		{
			name: "hyphenated compound word in title is not mistaken for a separator",
			stem: "Test-Driven Development By Example",
			want: Result{Title: "Test-Driven Development By Example", Author: "", Year: ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Parse(tt.stem, junkTags)
			if got != tt.want {
				t.Errorf("Parse(%q) = %+v, want %+v", tt.stem, got, tt.want)
			}
		})
	}
}

func TestParse_NoSeparatorMeansTitleOnly(t *testing.T) {
	got := Parse("SomeBookTitle", nil)
	want := Result{Title: "SomeBookTitle"}
	if got != want {
		t.Errorf("Parse() = %+v, want %+v", got, want)
	}
}
