package main

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/FrancisChung/BLibOrg/internal/config"
	"github.com/FrancisChung/BLibOrg/internal/covercache"
)

// coverHandler serves cached cover images at /covers/<file> for the Library
// view's <img> tags. Wails 2.13 blocks file:// URLs in the webview (see
// App.OpenFile's doc comment), so covers can't be loaded as raw filesystem
// paths -- this same-origin route is the workaround. configPath is injected
// (rather than calling appapi.DefaultConfigPath directly) so tests can point
// it at a temp config, matching appapi.App's own configPath field. The log
// folder is resolved fresh on every request rather than cached at startup,
// matching the rest of the app's "always reload config" convention.
func coverHandler(configPath func() (string, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/covers/")
		if name == "" || strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
			http.NotFound(w, r)
			return
		}

		cfgPath, err := configPath()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		cfg, err := config.Load(cfgPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		http.ServeFile(w, r, filepath.Join(covercache.Dir(cfg.General.LogFolder), name))
	})
}
