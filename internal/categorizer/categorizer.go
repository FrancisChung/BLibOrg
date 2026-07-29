package categorizer

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/FrancisChung/BLibOrg/internal/book"
	"github.com/FrancisChung/BLibOrg/internal/config"
)

const UncategorizedName = "Uncategorized"

// Categorize sets b.Category and b.Subcategory in place: config.Rules are
// evaluated top-to-bottom (first match wins) against author/metadata_subject
// (case-insensitive exact match) or title/filename (regex); if nothing
// matches, it falls back to the embedded genre/subject metadata against
// configured subcategory names; if that also fails, Uncategorized.
func Categorize(b *book.Book, cfg config.Config) {
	if b.CategoryManual {
		return
	}

	filename := filepath.Base(b.SourcePath)

	for _, rule := range cfg.Rules {
		matched := false
		switch rule.MatchField {
		case "author":
			matched = strings.EqualFold(strings.TrimSpace(b.Author.Value), strings.TrimSpace(rule.MatchValue))
		case "title":
			if re, err := regexp.Compile(rule.MatchValue); err == nil {
				matched = re.MatchString(b.Title.Value)
			}
		case "metadata_subject":
			matched = strings.EqualFold(strings.TrimSpace(b.Subject), strings.TrimSpace(rule.MatchValue))
		case "filename":
			if re, err := regexp.Compile(rule.MatchValue); err == nil {
				matched = re.MatchString(filename)
			}
		default:
			continue
		}
		if matched {
			b.Category = rule.Category
			b.Subcategory = rule.Subcategory
			b.CategoryWarning = undeclaredCategoryWarning(cfg, rule.Category, rule.Subcategory)
			return
		}
	}

	if b.Subject != "" {
		for catName, cat := range cfg.Categories {
			for _, sub := range cat.Subcategories {
				if strings.EqualFold(sub, b.Subject) {
					b.Category = catName
					b.Subcategory = sub
					return
				}
			}
		}
	}

	b.Category = UncategorizedName
	b.Subcategory = ""
}

// undeclaredCategoryWarning reports (without altering the move) when a
// matched rule points at a category or subcategory that isn't declared
// under cfg.Categories -- the rule still wins and the book still gets
// filed there, but the caller (UI layer) has something to surface.
func undeclaredCategoryWarning(cfg config.Config, category, subcategory string) string {
	cat, ok := cfg.Categories[category]
	if !ok {
		return fmt.Sprintf("rule matched undeclared category %q", category)
	}
	if subcategory == "" {
		return ""
	}
	for _, sub := range cat.Subcategories {
		if strings.EqualFold(sub, subcategory) {
			return ""
		}
	}
	return fmt.Sprintf("rule matched undeclared subcategory %q under category %q", subcategory, category)
}
