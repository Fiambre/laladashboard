package handlers

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/rfguerreroa/laladashboard/internal/config"
	"github.com/rfguerreroa/laladashboard/internal/registry"
	"github.com/rfguerreroa/laladashboard/internal/widgets"
)

type frameServer interface {
	ServeFrame(w http.ResponseWriter, r *http.Request, inst widgets.WidgetInstance)
}

func WidgetFrame(store *config.Store, reg *registry.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "widgetID")
		cfg := store.Get()

		for _, inst := range cfg.Widgets {
			if inst.ID != id || !inst.Enabled {
				continue
			}
			widget, ok := reg.Get(inst.TypeID)
			if !ok {
				http.Error(w, "unknown widget type", http.StatusNotFound)
				return
			}
			fs, ok := widget.(frameServer)
			if !ok {
				http.Error(w, "widget does not support frames", http.StatusNotFound)
				return
			}
			inst.DataDir = filepath.Join("data", "widgets", inst.ID)
			os.MkdirAll(inst.DataDir, 0755) //nolint:errcheck
			fs.ServeFrame(w, r, inst)
			return
		}
		http.NotFound(w, r)
	}
}
