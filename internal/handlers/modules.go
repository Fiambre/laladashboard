package handlers

import (
	"encoding/json"
	"net/http"
	"regexp"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/rfguerreroa/laladashboard/internal/moduleloader"
	"github.com/rfguerreroa/laladashboard/internal/registry"
	"github.com/rfguerreroa/laladashboard/templates"
)

var validModuleID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func safeModuleID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "moduleID")
	if !validModuleID.MatchString(id) {
		http.Error(w, "invalid module id", http.StatusBadRequest)
		return "", false
	}
	return id, true
}

// requireLocalOrigin rejects requests that don't come from the dashboard itself (basic CSRF guard).
func requireLocalOrigin(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("X-Requested-With") != "laladashboard" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	return true
}

const modulesDir = "modules"

func ModuleStore(reg *registry.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		remote, err := moduleloader.GetRegistry()
		installed := moduleloader.InstalledIDs(modulesDir)
		templ.Handler(templates.ModuleStore(remote, installed, err)).ServeHTTP(w, r)
	}
}

func InstallModule(loader *moduleloader.Loader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireLocalOrigin(w, r) {
			return
		}
		moduleID, ok := safeModuleID(w, r)
		if !ok {
			return
		}
		remote, err := moduleloader.GetRegistry()
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
		if !requireLocalOrigin(w, r) {
			return
		}
		moduleID, ok := safeModuleID(w, r)
		if !ok {
			return
		}
		if err := moduleloader.UninstallModule(moduleID, modulesDir); err != nil {
			http.Error(w, "uninstall failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "uninstalled", "warning": "restart required to deactivate"})
	}
}

