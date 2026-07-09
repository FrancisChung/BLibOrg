package config

import "os"

import "gopkg.in/yaml.v3"

type Config struct {
	General    General             `yaml:"general"`
	Heuristics Heuristics          `yaml:"heuristics"`
	Categories map[string]Category `yaml:"categories"`
	Rules      []Rule              `yaml:"rules"`
}

type General struct {
	WorkingFolder  string    `yaml:"working_folder"`
	LibraryFolder  string    `yaml:"library_folder"`
	LogFolder      string    `yaml:"log_folder"`
	FilenameFormat string    `yaml:"filename_format"`
	Fallbacks      Fallbacks `yaml:"fallbacks"`
}

type Fallbacks struct {
	Year   string `yaml:"year"`
	Author string `yaml:"author"`
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

func Save(path string, cfg Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
