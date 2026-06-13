package moduleloader

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/a-h/templ"
	"github.com/rfguerreroa/laladashboard/internal/registry"
	"github.com/rfguerreroa/laladashboard/internal/widgets"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	wasi "github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// ModuleManifest is read from manifest.json next to the .wasm file.
type ModuleManifest struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	Version     string               `json:"version"`
	Author      string               `json:"author"`
	Description string               `json:"description"`
	Wasm        string               `json:"wasm"`
	Schema      []widgets.ConfigField `json:"schema"`
}

// Loader holds the Wazero runtime and all loaded WASM modules.
type Loader struct {
	runtime wazero.Runtime
}

// wasmMu serialises all WASM render calls. Re-instantiation is not concurrent-safe.
var wasmMu sync.Mutex

// instanceCounter provides unique names for module instances.
var instanceCounter atomic.Uint64

func New(ctx context.Context) *Loader {
	rt := wazero.NewRuntime(ctx)
	wasi.MustInstantiate(ctx, rt)
	return &Loader{runtime: rt}
}

// Close releases the Wazero runtime.
func (l *Loader) Close(ctx context.Context) {
	l.runtime.Close(ctx)
}

// LoadAll scans modulesDir for subdirectories containing manifest.json + .wasm,
// compiles each module, and registers it in the global registry.
func (l *Loader) LoadAll(ctx context.Context, modulesDir string) error {
	entries, err := os.ReadDir(modulesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // modules/ directory is optional
		}
		return err
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(modulesDir, e.Name())
		if err := l.loadModule(ctx, dir); err != nil {
			log.Printf("[moduleloader] failed to load %s: %v", e.Name(), err)
		}
	}
	return nil
}

// LoadModule is exported for hot-loading a single module directory.
func (l *Loader) LoadModule(ctx context.Context, dir string) error {
	return l.loadModule(ctx, dir)
}

func (l *Loader) loadModule(ctx context.Context, dir string) error {
	manifestPath := filepath.Join(dir, "manifest.json")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("missing manifest.json: %w", err)
	}
	var manifest ModuleManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return fmt.Errorf("invalid manifest.json: %w", err)
	}

	wasmFile := manifest.Wasm
	if wasmFile == "" {
		wasmFile = "widget.wasm"
	}
	wasmBytes, err := os.ReadFile(filepath.Join(dir, wasmFile))
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", wasmFile, err)
	}

	// Build host module — may already exist from a previous module load.
	hostBuilder := l.runtime.NewHostModuleBuilder("env")
	hostBuilder.NewFunctionBuilder().WithFunc(l.hostHTTPGet).Export("http_get")
	hostBuilder.NewFunctionBuilder().WithFunc(l.hostHTTPPost).Export("http_post")
	hostBuilder.NewFunctionBuilder().WithFunc(l.hostHTTPPostAuth).Export("http_post_auth")
	hostBuilder.NewFunctionBuilder().WithFunc(l.hostHTTPCheck).Export("http_check")
	hostBuilder.NewFunctionBuilder().WithFunc(l.hostLog).Export("log_message")
	if _, err := hostBuilder.Instantiate(ctx); err != nil {
		_ = err // ignore "already instantiated" on subsequent loads
	}

	compiled, err := l.runtime.CompileModule(ctx, wasmBytes)
	if err != nil {
		return fmt.Errorf("compile error: %w", err)
	}

	displayName := manifest.Name
	if displayName == "" {
		displayName = manifest.ID
	}

	w := &wasmWidget{
		typeID:      manifest.ID,
		displayName: displayName,
		schema:      manifest.Schema,
		compiled:    compiled,
		loader:      l,
	}
	registry.Register(w)
	log.Printf("[moduleloader] loaded WASM module: %s (%s)", manifest.ID, manifest.Version)
	return nil
}

// callRender spawns a fresh module instance, pipes configJSON via stdin, and
// captures rendered HTML from stdout. This is the correct wasip1 "command" model.
func (l *Loader) callRender(ctx context.Context, compiled wazero.CompiledModule, moduleID string, configJSON []byte) string {
	var stdout bytes.Buffer

	name := fmt.Sprintf("%s-%d", moduleID, instanceCounter.Add(1))
	mod, err := l.runtime.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().
		WithName(name).
		WithStdin(bytes.NewReader(configJSON)).
		WithStdout(&stdout).
		WithStderr(log.Writer()))
	if err != nil {
		return fmt.Sprintf("<p>render error: %s</p>", err.Error())
	}
	defer mod.Close(ctx)

	return stdout.String()
}

// safeExternalURL returns an error if rawURL targets a private/loopback address
// or uses a non-HTTP(S) scheme. This prevents WASM modules from using host
// HTTP functions as an SSRF proxy into internal network services.
func safeExternalURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme %q not allowed", u.Scheme)
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("requests to private/loopback addresses are not allowed")
		}
	}
	switch host {
	case "localhost", "metadata.google.internal":
		return fmt.Errorf("requests to %q are not allowed", host)
	}
	return nil
}

// Host functions implementations

func (l *Loader) hostHTTPGet(ctx context.Context, mod api.Module, urlPtr, urlLen, resultPtr uint32) uint32 {
	urlBytes, ok := mod.Memory().Read(urlPtr, urlLen)
	if !ok {
		return 0
	}
	rawURL := string(urlBytes)
	if err := safeExternalURL(rawURL); err != nil {
		log.Printf("[wasm/%s] http_get blocked: %v", mod.Name(), err)
		return 0
	}
	resp, err := http.Get(rawURL) //nolint:gosec
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	mod.Memory().Write(resultPtr, body)
	return uint32(len(body))
}

func (l *Loader) hostHTTPPost(ctx context.Context, mod api.Module, urlPtr, urlLen, bodyPtr, bodyLen, resultPtr uint32) uint32 {
	urlBytes, ok := mod.Memory().Read(urlPtr, urlLen)
	if !ok {
		return 0
	}
	rawURL := string(urlBytes)
	if err := safeExternalURL(rawURL); err != nil {
		log.Printf("[wasm/%s] http_post blocked: %v", mod.Name(), err)
		return 0
	}
	bodyBytes, ok := mod.Memory().Read(bodyPtr, bodyLen)
	if !ok {
		return 0
	}
	resp, err := http.Post(rawURL, "application/json", byteReader(bodyBytes)) //nolint:gosec
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	result, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	mod.Memory().Write(resultPtr, result)
	return uint32(len(result))
}

func (l *Loader) hostHTTPPostAuth(ctx context.Context, mod api.Module, urlPtr, urlLen, bodyPtr, bodyLen, authPtr, authLen, resultPtr uint32) uint32 {
	urlBytes, ok := mod.Memory().Read(urlPtr, urlLen)
	if !ok {
		return 0
	}
	rawURL := string(urlBytes)
	if err := safeExternalURL(rawURL); err != nil {
		log.Printf("[wasm/%s] http_post_auth blocked: %v", mod.Name(), err)
		return 0
	}
	bodyBytes, ok := mod.Memory().Read(bodyPtr, bodyLen)
	if !ok {
		return 0
	}
	authBytes, ok := mod.Memory().Read(authPtr, authLen)
	if !ok {
		return 0
	}
	req, err := http.NewRequestWithContext(ctx, "POST", rawURL, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return 0
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "bearer "+string(authBytes))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	result, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	mod.Memory().Write(resultPtr, result)
	return uint32(len(result))
}

func (l *Loader) hostHTTPCheck(_ context.Context, mod api.Module, urlPtr, urlLen uint32) uint32 {
	urlBytes, ok := mod.Memory().Read(urlPtr, urlLen)
	if !ok {
		return 0
	}
	rawURL := string(urlBytes)
	if err := safeExternalURL(rawURL); err != nil {
		log.Printf("[wasm/%s] http_check blocked: %v", mod.Name(), err)
		return 0
	}
	// Skip TLS verification so self-signed certs on internal servers don't cause false DOWN.
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
	}
	req, err := http.NewRequest("HEAD", rawURL, nil) //nolint:gosec
	if err != nil {
		return 0
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	resp.Body.Close()
	rtt := uint32(time.Since(start).Milliseconds())
	if rtt == 0 {
		rtt = 1
	}
	return rtt
}

func (l *Loader) hostLog(_ context.Context, mod api.Module, msgPtr, msgLen uint32) {
	b, ok := mod.Memory().Read(msgPtr, msgLen)
	if !ok {
		return
	}
	log.Printf("[wasm/%s] %s", mod.Name(), string(b))
}

func byteReader(b []byte) io.Reader {
	return &byteReader2{data: b}
}

type byteReader2 struct {
	data []byte
	pos  int
}

func (r *byteReader2) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// wasmWidget implements widgets.Widget by delegating to a WASM module.
type wasmWidget struct {
	typeID      string
	displayName string
	schema      []widgets.ConfigField
	compiled    wazero.CompiledModule
	loader      *Loader
}

func (w *wasmWidget) TypeID() string      { return w.typeID }
func (w *wasmWidget) DisplayName() string { return w.displayName }

func (w *wasmWidget) Render(ctx context.Context, inst widgets.WidgetInstance) templ.Component {
	return w.RenderContent(ctx, inst)
}

func (w *wasmWidget) RenderContent(ctx context.Context, inst widgets.WidgetInstance) templ.Component {
	configJSON, _ := json.Marshal(inst.Settings)
	wasmMu.Lock()
	html := w.loader.callRender(ctx, w.compiled, w.typeID, configJSON)
	wasmMu.Unlock()
	return templ.Raw(html)
}

func (w *wasmWidget) ConfigSchema() []widgets.ConfigField {
	return w.schema
}
