package moduleloader

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var validID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

const RegistryURL = "https://raw.githubusercontent.com/Fiambre/laladashboard-modules/main/registry.json"

// in-memory cache for the remote registry
var (
	regCache   Registry
	regErr     error
	regCacheAt time.Time
	regMu      sync.RWMutex
	regTTL     = 10 * time.Minute
)

// WarmCache fetches the remote registry in the background at startup so the
// first visit to /modules is instant.
func WarmCache() {
	go func() {
		reg, err := FetchRegistry()
		regMu.Lock()
		regCache, regErr, regCacheAt = reg, err, time.Now()
		regMu.Unlock()
		if err != nil {
			log.Printf("[registry] background fetch failed: %v", err)
		} else {
			log.Printf("[registry] cached %d modules", len(reg.Modules))
		}
	}()
}

// GetRegistry returns the registry from cache when fresh, fetches otherwise.
// On fetch error it falls back to stale cache so the page stays usable.
func GetRegistry() (Registry, error) {
	regMu.RLock()
	fresh := !regCacheAt.IsZero() && time.Since(regCacheAt) < regTTL
	cached, cachedErr, hasModules := regCache, regErr, len(regCache.Modules) > 0
	regMu.RUnlock()

	if fresh {
		return cached, cachedErr
	}

	reg, err := FetchRegistry()
	regMu.Lock()
	regCache, regErr, regCacheAt = reg, err, time.Now()
	regMu.Unlock()

	if err != nil && hasModules {
		// Return stale cache rather than a blank error page
		return cached, nil
	}
	return reg, err
}

type RemoteModule struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	Version     string   `json:"version"`
	Tags        []string `json:"tags"`
	WasmURL     string   `json:"wasm_url"`
	ManifestURL string   `json:"manifest_url"`
	SourceURL   string   `json:"source_url"`
}

type Registry struct {
	Version string         `json:"version"`
	Modules []RemoteModule `json:"modules"`
}

func FetchRegistry() (Registry, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(RegistryURL)
	if err != nil {
		return Registry{}, fmt.Errorf("cannot fetch registry: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Registry{}, fmt.Errorf("registry returned %d", resp.StatusCode)
	}
	var reg Registry
	if err := json.NewDecoder(resp.Body).Decode(&reg); err != nil {
		return Registry{}, err
	}
	return reg, nil
}

// InstallModule downloads the manifest and wasm for a remote module into modulesDir.
func InstallModule(mod RemoteModule, modulesDir string) error {
	if !validID.MatchString(mod.ID) {
		return fmt.Errorf("invalid module id: %q", mod.ID)
	}
	dest := filepath.Join(modulesDir, mod.ID)
	// Guard against path traversal after Join
	base, _ := filepath.Abs(modulesDir)
	abs, _ := filepath.Abs(dest)
	if !strings.HasPrefix(abs, base+string(filepath.Separator)) {
		return fmt.Errorf("path traversal detected for module id: %q", mod.ID)
	}
	if err := os.MkdirAll(dest, 0755); err != nil {
		return err
	}

	if err := downloadFile(mod.ManifestURL, filepath.Join(dest, "manifest.json")); err != nil {
		return fmt.Errorf("manifest download failed: %w", err)
	}
	if err := downloadFile(mod.WasmURL, filepath.Join(dest, "widget.wasm")); err != nil {
		return fmt.Errorf("wasm download failed: %w", err)
	}
	return nil
}

// UninstallModule removes the module directory from modulesDir.
func UninstallModule(moduleID, modulesDir string) error {
	dest := filepath.Join(modulesDir, moduleID)
	return os.RemoveAll(dest)
}

// InstalledIDs returns the set of module IDs currently installed in modulesDir.
func InstalledIDs(modulesDir string) map[string]bool {
	result := make(map[string]bool)
	entries, err := os.ReadDir(modulesDir)
	if err != nil {
		return result
	}
	for _, e := range entries {
		if e.IsDir() {
			result[e.Name()] = true
		}
	}
	return result
}

func downloadFile(url, dest string) error {
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(url) //nolint:gosec
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}
