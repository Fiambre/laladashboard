package handlers

import (
	"net/http"

	"github.com/a-h/templ"
	"github.com/rfguerreroa/laladashboard/internal/config"
	"github.com/rfguerreroa/laladashboard/internal/registry"
	"github.com/rfguerreroa/laladashboard/templates"
)

func ConfigPage(store *config.Store, reg *registry.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := store.Get()
		templ.Handler(templates.Config(cfg, reg)).ServeHTTP(w, r)
	}
}
