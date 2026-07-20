package appapi

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/FrancisChung/book-organiser/internal/categorizer"
	"github.com/FrancisChung/book-organiser/internal/config"
)

// DefaultConfigPath returns the fixed, OS-standard location config.yaml is
// read from: <user config dir>/book-organiser/config.yaml.
func DefaultConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "book-organiser", "config.yaml"), nil
}

// App is the pure-Go adapter the desktop app's Wails-bound struct
// delegates to. configPath is a field (not a direct call to
// DefaultConfigPath) so tests can point it at a temp file.
type App struct {
	configPath func() (string, error)
}

func NewApp() *App {
	return &App{configPath: DefaultConfigPath}
}

// ConfigStatusView reports the resolved config path and, if loading it
// failed, why -- so the UI can show a precise "no config found at <path>"
// message instead of a blank screen.
type ConfigStatusView struct {
	Path     string   `json:"path"`
	Error    string   `json:"error"`
	Warnings []string `json:"warnings"`
}

func (a *App) ConfigStatus() ConfigStatusView {
	path, err := a.configPath()
	if err != nil {
		return ConfigStatusView{Error: err.Error()}
	}
	cfg, err := config.Load(path)
	if err != nil {
		return ConfigStatusView{Path: path, Error: err.Error()}
	}
	return ConfigStatusView{Path: path, Warnings: config.ValidateRules(cfg)}
}

// loadConfig resolves the config path and loads it, for use by Scan,
// Recompute, and Apply.
func (a *App) loadConfig() (config.Config, error) {
	path, err := a.configPath()
	if err != nil {
		return config.Config{}, err
	}
	return config.Load(path)
}

// DestinationView is one selectable category/subcategory leaf a book can be
// manually routed to, as declared under config.yaml's categories section.
// Subcategory is "" for a category with no subcategories declared.
type DestinationView struct {
	Category    string `json:"category"`
	Subcategory string `json:"subcategory"`
}

// Categories returns every category/subcategory leaf declared in
// config.yaml, sorted by Category then Subcategory, excluding
// "Uncategorized" itself (never a valid manual destination). This backs
// the Scan & Review UI's destination-picker dropdown for Uncategorized
// books. Go map iteration order is randomized, so the sort is required for
// a stable, non-jumping dropdown across calls.
func (a *App) Categories() ([]DestinationView, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return nil, err
	}
	var dests []DestinationView
	for name, cat := range cfg.Categories {
		if name == categorizer.UncategorizedName {
			continue
		}
		if len(cat.Subcategories) == 0 {
			dests = append(dests, DestinationView{Category: name})
			continue
		}
		for _, sub := range cat.Subcategories {
			dests = append(dests, DestinationView{Category: name, Subcategory: sub})
		}
	}
	sort.Slice(dests, func(i, j int) bool {
		if dests[i].Category != dests[j].Category {
			return dests[i].Category < dests[j].Category
		}
		return dests[i].Subcategory < dests[j].Subcategory
	})
	return dests, nil
}
