package main

import (
	"context"
	"embed"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/rfguerreroa/laladashboard/internal/config"
	"github.com/rfguerreroa/laladashboard/internal/moduleloader"
	"github.com/rfguerreroa/laladashboard/internal/registry"
	"github.com/rfguerreroa/laladashboard/internal/server"

	// Built-in widgets registered via init()
	_ "github.com/rfguerreroa/laladashboard/internal/widgets/clock"
	_ "github.com/rfguerreroa/laladashboard/internal/widgets/iframe"
	_ "github.com/rfguerreroa/laladashboard/internal/widgets/rss"
	_ "github.com/rfguerreroa/laladashboard/internal/widgets/bashrunner"
	_ "github.com/rfguerreroa/laladashboard/internal/widgets/rtspgrabber"
	_ "github.com/rfguerreroa/laladashboard/internal/widgets/uptimechart"
	_ "github.com/rfguerreroa/laladashboard/internal/widgets/weather"
)

//go:embed static
var staticFS embed.FS

func main() {
	ctx := context.Background()

	if err := os.MkdirAll(filepath.Join("data", "widgets"), 0755); err != nil {
		log.Printf("warning: could not create data directory: %v", err)
	}

	store, err := config.NewStore("config/dashboard.json")
	if err != nil {
		log.Fatalf("failed to init config store: %v", err)
	}

	// Load external WASM modules from ./modules/
	loader := moduleloader.New(ctx)
	defer loader.Close(ctx)
	if err := loader.LoadAll(ctx, "modules"); err != nil {
		log.Printf("warning: module loader error: %v", err)
	}

	// Pre-fetch module registry in background so /modules loads instantly
	moduleloader.WarmCache()

	reg := registry.Global()
	handler := server.New(store, reg, loader, staticFS)

	addr := ":" + getEnv("PORT", "8080")
	log.Printf("LalaDashboard running on http://localhost%s", addr)
	log.Fatal(http.ListenAndServe(addr, handler))
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
