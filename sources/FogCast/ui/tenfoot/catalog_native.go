//go:build !wasip1 && !tamago

package tenfoot

import (
	"context"
	"strings"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/hostapi"
)

func configureLocalCatalog(app *App, opts Options) error {
	app.EnableKitLocal()
	if served := serveCatalog(opts.CatalogConfig); served != nil {
		if useLocalCatalog(opts.CatalogConfig, served.Notice) {
			app.client = NewClient(served.Base, launcherHTTPClient("", "")).withAPIHost("")
			app.localCatalogClose = served.Close
			app.localContent = served.ContentPath
		} else {
			_ = served.Close()
		}
	}
	return nil
}

// useLocalCatalog reports whether the booted kit-local catalog should
// replace the launcher.json host library (#616). The local shelf wins only
// when it has playable local files (an empty notice) and the catalog config
// does not declare targets that are all disabled. An empty, missing or
// offline shelf, or a config whose targets are all disabled (the image
// default), keeps the host. A config without targets keeps the #385
// hostless browse behaviour when it has local games.
func useLocalCatalog(configPath, notice string) bool {
	if strings.TrimSpace(notice) != "" {
		return false
	}
	cfg, err := fogcast.LoadConfig(configPath)
	if err != nil {
		return true
	}
	if len(cfg.Targets) == 0 {
		return true
	}
	for _, target := range cfg.Targets {
		if target.Enabled {
			return true
		}
	}
	return false
}

// serveCatalog boots the local catalog when configPath names a real file.
// A missing path or a failed boot returns nil and leaves the host API in place.
func serveCatalog(configPath string) *hostapi.LocalServe {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return nil
	}
	paths, err := fogcast.PathsForConfig(configPath)
	if err != nil {
		return nil
	}
	served, err := hostapi.ServeLocal(context.Background(), paths)
	if err != nil {
		return nil
	}
	return served
}
