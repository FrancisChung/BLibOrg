package main

import "testing"

func TestIsAffirmative(t *testing.T) {
	tests := []struct {
		name   string
		result string
		want   bool
	}{
		{"macOS custom button label", "Move files", true},
		{"macOS custom undo button label", "Undo", true},
		{"Linux/Windows default Yes (Buttons option is ignored there)", "Yes", true},
		{"generic OK fallback", "OK", true},
		{"macOS custom cancel label", "Cancel", false},
		{"Linux/Windows default No", "No", false},
		{"dialog dismissed with no selection", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAffirmative(tt.result); got != tt.want {
				t.Errorf("isAffirmative(%q) = %v, want %v", tt.result, got, tt.want)
			}
		})
	}
}
