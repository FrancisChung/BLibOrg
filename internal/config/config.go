package config

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

type Config struct {
	General    General             `yaml:"general"`
	Heuristics Heuristics          `yaml:"heuristics"`
	Categories map[string]Category `yaml:"categories"`
	Rules      []Rule              `yaml:"rules"`
}

type General struct {
	WorkingFolder  string `yaml:"working_folder"`
	LibraryFolder  string `yaml:"library_folder"`
	LogFolder      string `yaml:"log_folder"`
	FilenameFormat string `yaml:"filename_format"`
}

type Heuristics struct {
	KnownJunkTags []string `yaml:"known_junk_tags"`
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
