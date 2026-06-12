package moduleloader

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"github.com/a-h/templ"
	"github.com/rfguerreroa/laladashboard/internal/registry"
	"github.com/rfguerreroa/laladashboard/internal/widgets"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	wasi "github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// ModuleManifest is read from manifest.json next to the .wasm file.
type ModuleManifest struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	Author      string `json:"author"`
	Description string `json:"description"`
	Wasm        string `json:"wasm"`
}

// Loader holds the Wazero runtime and all loaded WASM modules.
type Loader struct {
	runtime wazero.Runtime
	mu      sync.Mutex
	results map[uint32]string // goroutine-local result store keyed by goroutine ID
}

// wasmMu serialises all WASM render calls. Wazero module instances share
// linear memory and are not safe for concurrent use.
var wasmMu sync.Mutex

func New(ctx context.Context) *Loader {
	rt := wazero.NewRuntime(ctx)
	wasi.MustInstantiate(ctx, rt)
	return &Loader{runtime: rt, results: make(map[uint32]string)}
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

	// Build host module with http_get, http_post, log_message functions
	hostBuilder := l.runtime.NewHostModuleBuilder("env")
	hostBuilder.NewFunctionBuilder().
		WithFunc(l.hostHTTPGet).
		Export("http_get")
	hostBuilder.NewFunctionBuilder().
		WithFunc(l.hostHTTPPost).
		Export("http_post")
	hostBuilder.NewFunctionBuilder().
		WithFunc(l.hostLog).
		Export("log_message")

	if _, err := hostBuilder.Instantiate(ctx); err != nil {
		// Host module may already exist from a previous module load — ignore duplicate error
		_ = err
	}

	compiled, err := l.runtime.CompileModule(ctx, wasmBytes)
	if err != nil {
		return fmt.Errorf("compile error: %w", err)
	}

	mod, err := l.runtime.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().
		WithName(manifest.ID).
		WithStdout(log.Writer()).
		WithStderr(log.Writer()))
	if err != nil {
		return fmt.Errorf("instantiate error: %w", err)
	}

	// Retrieve display name from module
	displayName := manifest.ID
	if fn := mod.ExportedFunction("module_name"); fn != nil {
		results, err := fn.Call(ctx)
		if err == nil && len(results) > 0 {
			displayName = l.readModuleString(ctx, mod, uint32(results[0]))
		}
	}

	// Retrieve config schema from module
	var schema []widgets.ConfigField
	if fn := mod.ExportedFunction("config_schema"); fn != nil {
		results, err := fn.Call(ctx)
		if err == nil && len(results) > 0 {
			schemaJSON := l.readModuleString(ctx, mod, uint32(results[0]))
			_ = json.Unmarshal([]byte(schemaJSON), &schema)
		}
	}

	w := &wasmWidget{
		typeID:      manifest.ID,
		displayName: displayName,
		schema:      schema,
		mod:         mod,
		loader:      l,
	}
	registry.Register(w)
	log.Printf("[moduleloader] loaded WASM module: %s (%s)", manifest.ID, manifest.Version)
	return nil
}

// readModuleString reads a null-terminated or length-prefixed string from WASM memory.
// Our convention: the module exports get_output_ptr/get_output_len after each call.
func (l *Loader) readModuleString(ctx context.Context, mod api.Module, _ uint32) string {
	ptrFn := mod.ExportedFunction("get_output_ptr")
	lenFn := mod.ExportedFunction("get_output_len")
	if ptrFn == nil || lenFn == nil {
		return ""
	}
	ptrRes, _ := ptrFn.Call(ctx)
	lenRes, _ := lenFn.Call(ctx)
	if len(ptrRes) == 0 || len(lenRes) == 0 {
		return ""
	}
	ptr := uint32(ptrRes[0])
	length := uint32(lenRes[0])
	b, ok := mod.Memory().Read(ptr, length)
	if !ok {
		return ""
	}
	return string(b)
}

// callRender writes config JSON into WASM memory, calls render(), reads output.
func (l *Loader) callRender(ctx context.Context, mod api.Module, configJSON []byte) string {
	allocFn := mod.ExportedFunction("alloc")
	renderFn := mod.ExportedFunction("render")
	if renderFn == nil {
		return "<p>widget has no render function</p>"
	}

	var cfgPtr, cfgLen uint64
	if allocFn != nil {
		res, err := allocFn.Call(ctx, uint64(len(configJSON)))
		if err != nil || len(res) == 0 {
			return "<p>alloc failed</p>"
		}
		cfgPtr = res[0]
		cfgLen = uint64(len(configJSON))
		if !mod.Memory().Write(uint32(cfgPtr), configJSON) {
			return "<p>memory write failed</p>"
		}
	} else {
		// Module doesn't export alloc — write to a fixed offset (offset 0 after stack)
		cfgPtr = 65536
		cfgLen = uint64(len(configJSON))
		mod.Memory().Write(uint32(cfgPtr), configJSON)
	}

	if _, err := renderFn.Call(ctx, cfgPtr, cfgLen); err != nil {
		return fmt.Sprintf("<p>render error: %s</p>", err.Error())
	}

	return l.readModuleString(ctx, mod, 0)
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
	mod         api.Module
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
	html := w.loader.callRender(ctx, w.mod, configJSON)
	wasmMu.Unlock()
	return templ.Raw(html)
}

func (w *wasmWidget) ConfigSchema() []widgets.ConfigField {
	return w.schema
}
