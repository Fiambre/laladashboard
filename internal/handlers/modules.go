package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/a-h/templ"
	"github.com/rfguerreroa/laladashboard/internal/moduleloader"
	"github.com/rfguerreroa/laladashboard/internal/registry"
	"github.com/rfguerreroa/laladashboard/templates"
)

const modulesDir = "modules"

func ModuleStore(reg *registry.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		remote, err := moduleloader.FetchRegistry()
		if err != nil {
			// Render page with error, don't fail hard
			remote = moduleloader.Registry{}
		}
		installed := moduleloader.InstalledIDs(modulesDir)
		templ.Handler(templates.ModuleStore(remote, installed, err)).ServeHTTP(w, r)
	}
}

func InstallModule(loader *moduleloader.Loader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		moduleID := chi.URLParam(r, "moduleID")

		remote, err := moduleloader.FetchRegistry()
		if err != nil {
			http.Error(w, "cannot fetch registry: "+err.Error(), http.StatusServiceUnavailable)
			return
		}

		var found *moduleloader.RemoteModule
		for i, m := range remote.Modules {
			if m.ID == moduleID {
				found = &remote.Modules[i]
				break
			}
		}
		if found == nil {
			http.Error(w, "module not found in registry", http.StatusNotFound)
			return
		}

		if err := moduleloader.InstallModule(*found, modulesDir); err != nil {
			http.Error(w, "install failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Hot-load the new module without restart
		if err := loader.LoadModule(r.Context(), "modules/"+moduleID); err != nil {
			// Module saved but couldn't hot-load — restart needed
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"status":  "installed",
				"warning": "restart required to activate",
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "installed"})
	}
}

func UninstallModule(loader *moduleloader.Loader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		moduleID := chi.URLParam(r, "moduleID")
		if err := moduleloader.UninstallModule(moduleID, modulesDir); err != nil {
			http.Error(w, "uninstall failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "uninstalled", "warning": "restart required to deactivate"})
	}
}

// unused import guard
var _ = context.Background
