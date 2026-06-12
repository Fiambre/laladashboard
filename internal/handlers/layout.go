package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rfguerreroa/laladashboard/internal/config"
	"github.com/rfguerreroa/laladashboard/internal/registry"
	"github.com/rfguerreroa/laladashboard/internal/widgets"
)

func newID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func SaveLayout(store *config.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var positions []config.WidgetLayout
		if err := json.NewDecoder(r.Body).Decode(&positions); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		if err := store.UpdateLayout(positions); err != nil {
			http.Error(w, "failed to save layout", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"saved"}`))
	}
}

func AddWidget(store *config.Store, reg *registry.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			TypeID   string            `json:"type_id"`
			Title    string            `json:"title"`
			Settings map[string]string `json:"settings"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		wt, ok := reg.Get(body.TypeID)
		if !ok {
			http.Error(w, "unknown widget type", http.StatusBadRequest)
			return
		}
		// Fill defaults for missing settings
		settings := map[string]string{}
		for _, f := range wt.ConfigSchema() {
			settings[f.Key] = f.Default
		}
		for k, v := range body.Settings {
			settings[k] = v
		}
		title := body.Title
		if title == "" {
			title = wt.DisplayName()
		}
		inst := widgets.WidgetInstance{
			ID:       newID(),
			TypeID:   body.TypeID,
			Title:    title,
			Enabled:  true,
			Settings: settings,
			Grid:     widgets.GridPosition{X: 0, Y: 0, W: 3, H: 2},
		}
		if err := store.AddWidget(inst); err != nil {
			http.Error(w, "failed to add widget", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(inst)
	}
}

func RemoveWidget(store *config.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "widgetID")
		if err := store.RemoveWidget(id); err != nil {
			http.Error(w, "widget not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"removed"}`))
	}
}

func SaveWidgetSettings(store *config.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "widgetID")
		var settings map[string]string
		if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		if err := store.UpdateSettings(id, settings); err != nil {
			http.Error(w, "widget not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"saved"}`))
	}
}

func GetConfigVersion(store *config.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(map[string]int64{"version": store.Version()})
	}
}

func SetTheme(store *config.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Theme string `json:"theme"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		if body.Theme != "dark" && body.Theme != "light" {
			http.Error(w, "invalid theme", http.StatusBadRequest)
			return
		}
		if err := store.SetTheme(body.Theme); err != nil {
			http.Error(w, "failed to save theme", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"saved"}`))
	}
}
