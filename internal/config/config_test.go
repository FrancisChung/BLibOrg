package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleYAML = `
general:
  working_folder: "/inbox"
  library_folder: "/library"
  log_folder: "/library/.book-organiser-logs"
  filename_format: "{title} ({year}) - {author}"

heuristics:
  known_junk_tags: ["OceanofPDF.com", "libgen.li"]

title_formatting:
  hyphen_exceptions: ["High-Performance", "Domain-Driven"]

categories:
  Fiction:
    subcategories: [Sci-Fi, Fantasy]
  Uncategorized:
    subcategories: []

rules:
  - match_field: author
    match_value: "Isaac Asimov"
    category: Fiction
    subcategory: Sci-Fi
`

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(sampleYAML), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.General.WorkingFolder != "/inbox" {
		t.Errorf("WorkingFolder = %q, want /inbox", cfg.General.WorkingFolder)
	}
	if cfg.General.LogFolder != "/library/.book-organiser-logs" {
		t.Errorf("LogFolder = %q, want /library/.book-organiser-logs", cfg.General.LogFolder)
	}
	if cfg.General.FilenameFormat != "{title} ({year}) - {author}" {
		t.Errorf("FilenameFormat = %q", cfg.General.FilenameFormat)
	}
	if len(cfg.Heuristics.KnownJunkTags) != 2 || cfg.Heuristics.KnownJunkTags[0] != "OceanofPDF.com" {
		t.Errorf("KnownJunkTags = %v", cfg.Heuristics.KnownJunkTags)
	}
	if len(cfg.TitleFormatting.HyphenExceptions) != 2 || cfg.TitleFormatting.HyphenExceptions[0] != "High-Performance" {
		t.Errorf("TitleFormatting.HyphenExceptions = %v", cfg.TitleFormatting.HyphenExceptions)
	}
	fiction, ok := cfg.Categories["Fiction"]
	if !ok || len(fiction.Subcategories) != 2 || fiction.Subcategories[0] != "Sci-Fi" {
		t.Errorf("Categories[Fiction] = %+v, ok=%v", fiction, ok)
	}
	if len(cfg.Rules) != 1 || cfg.Rules[0].MatchValue != "Isaac Asimov" || cfg.Rules[0].Category != "Fiction" {
		t.Errorf("Rules = %+v", cfg.Rules)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	original := Config{
		General: General{
			WorkingFolder:  "/inbox",
			LibraryFolder:  "/library",
			LogFolder:      "/library/.book-organiser-logs",
			FilenameFormat: "{title} ({year}) - {author}",
		},
		Heuristics: Heuristics{KnownJunkTags: []string{"z-lib.org"}},
		Categories: map[string]Category{
			"NonFiction": {Subcategories: []string{"Technology"}},
		},
		Rules: []Rule{
			{MatchField: "filename", MatchValue: "(?i)docker", Category: "NonFiction", Subcategory: "Technology"},
		},
	}

	if err := Save(path, original); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if loaded.General != original.General {
		t.Errorf("General round-trip mismatch:\n got  %+v\n want %+v", loaded.General, original.General)
	}
	if loaded.Rules[0] != original.Rules[0] {
		t.Errorf("Rules round-trip mismatch:\n got  %+v\n want %+v", loaded.Rules[0], original.Rules[0])
	}
}

func TestValidateRules_InvalidRegexReported(t *testing.T) {
	cfg := Config{
		Rules: []Rule{
			{MatchField: "filename", MatchValue: "(?i)docker"},
			{MatchField: "filename", MatchValue: `(?i)\bc++\b`},
			{MatchField: "author", MatchValue: "++broken but not a filename rule"},
		},
	}

	warnings := ValidateRules(cfg)
	if len(warnings) != 1 {
		t.Fatalf("ValidateRules() = %v, want exactly 1 warning", warnings)
	}
	if !strings.Contains(warnings[0], "rule 2") || !strings.Contains(warnings[0], "c++") {
		t.Errorf("warning = %q, want it to identify rule 2 and its pattern", warnings[0])
	}
}

func TestValidateRules_AllValidReturnsNoWarnings(t *testing.T) {
	cfg := Config{
		Rules: []Rule{
			{MatchField: "filename", MatchValue: "(?i)docker"},
			{MatchField: "author", MatchValue: "Isaac Asimov"},
		},
	}

	if warnings := ValidateRules(cfg); len(warnings) != 0 {
		t.Errorf("ValidateRules() = %v, want none", warnings)
	}
}
