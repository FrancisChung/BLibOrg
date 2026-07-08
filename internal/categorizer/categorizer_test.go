package categorizer

import (
	"testing"

	"github.com/FrancisChung/book-organiser/internal/book"
	"github.com/FrancisChung/book-organiser/internal/config"
)

func testConfig() config.Config {
	return config.Config{
		Categories: map[string]config.Category{
			"Fiction":    {Subcategories: []string{"Sci-Fi", "Fantasy"}},
			"NonFiction": {Subcategories: []string{"Technology"}},
			"Uncategorized": {},
		},
		Rules: []config.Rule{
			{MatchField: "author", MatchValue: "Isaac Asimov", Category: "Fiction", Subcategory: "Sci-Fi"},
			{MatchField: "filename", MatchValue: "(?i)docker|kubernetes", Category: "NonFiction", Subcategory: "Technology"},
		},
	}
}

func TestCategorize_RuleMatchOnAuthor(t *testing.T) {
	b := &book.Book{SourcePath: "/inbox/foundation.epub", Author: book.Field{Value: "Isaac Asimov"}}
	Categorize(b, testConfig())
	if b.Category != "Fiction" || b.Subcategory != "Sci-Fi" {
		t.Errorf("Category/Subcategory = %s/%s, want Fiction/Sci-Fi", b.Category, b.Subcategory)
	}
}

func TestCategorize_RuleMatchOnFilenameRegex(t *testing.T) {
	b := &book.Book{SourcePath: "/inbox/Learning Docker.epub", Author: book.Field{Value: "Someone Else"}}
	Categorize(b, testConfig())
	if b.Category != "NonFiction" || b.Subcategory != "Technology" {
		t.Errorf("Category/Subcategory = %s/%s, want NonFiction/Technology", b.Category, b.Subcategory)
	}
}

func TestCategorize_FirstRuleWins(t *testing.T) {
	cfg := testConfig()
	cfg.Rules = append([]config.Rule{
		{MatchField: "author", MatchValue: "Isaac Asimov", Category: "NonFiction", Subcategory: "Technology"},
	}, cfg.Rules...)
	b := &book.Book{SourcePath: "/inbox/foundation.epub", Author: book.Field{Value: "Isaac Asimov"}}
	Categorize(b, cfg)
	if b.Category != "NonFiction" {
		t.Errorf("Category = %s, want NonFiction (first matching rule)", b.Category)
	}
}

func TestCategorize_FallsBackToMetadataSubject(t *testing.T) {
	b := &book.Book{SourcePath: "/inbox/whatever.epub", Author: book.Field{Value: "Nobody Known"}, Subject: "Fantasy"}
	Categorize(b, testConfig())
	if b.Category != "Fiction" || b.Subcategory != "Fantasy" {
		t.Errorf("Category/Subcategory = %s/%s, want Fiction/Fantasy", b.Category, b.Subcategory)
	}
}

func TestCategorize_FallsBackToUncategorized(t *testing.T) {
	b := &book.Book{SourcePath: "/inbox/whatever.epub", Author: book.Field{Value: "Nobody Known"}}
	Categorize(b, testConfig())
	if b.Category != UncategorizedName {
		t.Errorf("Category = %s, want %s", b.Category, UncategorizedName)
	}
	if b.Subcategory != "" {
		t.Errorf("Subcategory = %s, want empty", b.Subcategory)
	}
}
