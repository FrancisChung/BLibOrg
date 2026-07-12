package appapi

import (
	"os"
	"path/filepath"

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
	Path  string `json:"path"`
	Error string `json:"error"`
}

func (a *App) ConfigStatus() ConfigStatusView {
	path, err := a.configPath()
	if err != nil {
		return ConfigStatusView{Error: err.Error()}
	}
	if _, err := config.Load(path); err != nil {
		return ConfigStatusView{Path: path, Error: err.Error()}
	}
	return ConfigStatusView{Path: path}
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
