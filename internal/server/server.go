package server

import (
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rfguerreroa/laladashboard/internal/config"
	"github.com/rfguerreroa/laladashboard/internal/handlers"
	"github.com/rfguerreroa/laladashboard/internal/moduleloader"
	"github.com/rfguerreroa/laladashboard/internal/registry"
)

func New(store *config.Store, reg *registry.Registry, loader *moduleloader.Loader, staticFS fs.FS) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))

	staticSub, _ := fs.Sub(staticFS, "static")
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	r.Get("/", handlers.Dashboard(store, reg))
	r.Get("/config", handlers.ConfigPage(store, reg))
	r.Get("/modules", handlers.ModuleStore(reg))

	r.Post("/api/layout", handlers.SaveLayout(store))
	r.Post("/api/widgets", handlers.AddWidget(store, reg))
	r.Delete("/api/widgets/{widgetID}", handlers.RemoveWidget(store))
	r.Put("/api/widgets/{widgetID}/settings", handlers.SaveWidgetSettings(store))
	r.Post("/api/theme", handlers.SetTheme(store))
	r.Post("/api/modules/{moduleID}/install", handlers.InstallModule(loader))
	r.Delete("/api/modules/{moduleID}/uninstall", handlers.UninstallModule(loader))

	r.Get("/widgets/{widgetID}/content", handlers.WidgetContent(store, reg))

	return r
}
