package config

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

type Config struct {
	General         General             `yaml:"general"`
	Heuristics      Heuristics          `yaml:"heuristics"`
	TitleFormatting TitleFormatting     `yaml:"title_formatting"`
	Categories      map[string]Category `yaml:"categories"`
	Rules           []Rule              `yaml:"rules"`
}

type General struct {
	WorkingFolder     string `yaml:"working_folder"`
	LibraryFolder     string `yaml:"library_folder"`
	LogFolder         string `yaml:"log_folder"`
	FilenameFormat    string `yaml:"filename_format"`
	PDFCoverPageLimit int    `yaml:"pdf_cover_page_limit"`
	// ScanConcurrency caps how many books internal/librarian.Scan
	// processes concurrently. 0 (the Go zero value, and what an unset
	// key in the user's config.yaml unmarshals to) means "use
	// runtime.NumCPU()" -- resolved at the point of use in Scan itself,
	// the same "<= 0 means use the built-in default" convention
	// PDFCoverPageLimit's own consumer (walkPDFPageTree) already uses.
	ScanConcurrency int `yaml:"scan_concurrency"`
}

type Heuristics struct {
	KnownJunkTags []string `yaml:"known_junk_tags"`
}

// TitleFormatting configures textutil.FormatTitle's hyphen-to-space
// conversion: HyphenExceptions lists hyphenated words to keep hyphenated
// (matched case-insensitively, substituted with the list's exact casing)
// instead of splitting on "-" into separate words.
type TitleFormatting struct {
	HyphenExceptions []string `yaml:"hyphen_exceptions"`
}

type Category struct {
	Subcategories []string `yaml:"subcategories"`
}

type Rule struct {
	MatchField  string `yaml:"match_field"`
	MatchValue  string `yaml:"match_value"`
	Category    string `yaml:"category"`
	Subcategory string `yaml:"subcategory"`
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// ValidateRules compiles every filename-matching rule's regex and returns a
// human-readable warning for each one that fails, so a typo'd pattern (e.g.
// unescaped regex metacharacters) doesn't silently never match instead of
// being reported.
func ValidateRules(cfg Config) []string {
	var warnings []string
	for i, rule := range cfg.Rules {
		if rule.MatchField != "filename" {
			continue
		}
		if _, err := regexp.Compile(rule.MatchValue); err != nil {
			warnings = append(warnings, fmt.Sprintf("rule %d (match_value %q): invalid regex: %v", i+1, rule.MatchValue, err))
		}
	}
	return warnings
}

func Save(path string, cfg Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
