package handlers

import (
	"net/http"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/rfguerreroa/laladashboard/internal/config"
	"github.com/rfguerreroa/laladashboard/internal/registry"
)

func WidgetContent(store *config.Store, reg *registry.Registry) http.HandlerFunc {
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
			templ.Handler(widget.RenderContent(r.Context(), inst)).ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	}
}
