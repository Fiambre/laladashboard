package rtspgrabber

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/a-h/templ"
	"github.com/rfguerreroa/laladashboard/internal/registry"
	"github.com/rfguerreroa/laladashboard/internal/widgets"
)

func init() {
	registry.Register(&RTSPGrabberWidget{})
}

// ── URL helpers ───────────────────────────────────────────────────────────────

type directKind string

const (
	directMJPEG directKind = "mjpeg"
	directHLS   directKind = "hls"
)

func detectDirectKind(u string) directKind {
	lower := strings.ToLower(u)
	if strings.HasSuffix(lower, ".m3u8") || strings.Contains(lower, ".m3u8?") {
		return directHLS
	}
	return directMJPEG
}

// ── go2rtc process manager (singleton) ───────────────────────────────────────

type go2rtcManager struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	baseURL string
	running bool
}

var g2m = &go2rtcManager{}

// ensure starts go2rtc if it is not already running.
// If go2rtc is already reachable at the configured port (started externally), it skips the launch.
func (m *go2rtcManager) ensure(binPath, port string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return nil
	}

	baseURL := "http://127.0.0.1:" + port

	// Already running externally — just adopt it.
	if ping(baseURL) {
		m.running = true
		m.baseURL = baseURL
		return nil
	}

	// Write a minimal config so go2rtc listens on the chosen port.
	cfgPath := filepath.Join(os.TempDir(), "lala-go2rtc.yaml")
	cfgContent := fmt.Sprintf("api:\n  listen: ':%s'\n  origin: '*'\n", port)
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0600); err != nil {
		return fmt.Errorf("error escribiendo config go2rtc: %w", err)
	}

	cmd := exec.Command(binPath, "-config", cfgPath) //nolint:gosec
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("no se pudo iniciar go2rtc (%q): %w", binPath, err)
	}
	m.cmd = cmd
	m.baseURL = baseURL

	// Wait up to 5 s for go2rtc to accept connections.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ping(baseURL) {
			m.running = true
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	m.cmd.Process.Kill() //nolint:errcheck
	m.cmd = nil
	return fmt.Errorf("go2rtc no respondió en 5 s (¿binario en '%s'?)", binPath)
}

func ping(baseURL string) bool {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get(baseURL + "/api")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 500
}

// ── Widget ────────────────────────────────────────────────────────────────────

type RTSPGrabberWidget struct {
	registered sync.Map // key: baseURL+"|"+streamName → struct{}
}

func (w *RTSPGrabberWidget) TypeID() string      { return "rtsp-grabber" }
func (w *RTSPGrabberWidget) DisplayName() string { return "Cámara RTSP / HTTP" }

func (w *RTSPGrabberWidget) Render(ctx context.Context, inst widgets.WidgetInstance) templ.Component {
	return w.RenderContent(ctx, inst)
}

func (w *RTSPGrabberWidget) RenderContent(ctx context.Context, inst widgets.WidgetInstance) templ.Component {
	streamURL := inst.Setting("stream_url", inst.Setting("rtsp_url", ""))
	if streamURL == "" {
		return rtspError("Configura la URL del stream")
	}

	switch inst.Setting("stream_type", "rtsp") {
	case "http":
		return rtspDirect(streamURL, detectDirectKind(streamURL))

	default: // "rtsp"
		binPath := inst.Setting("go2rtc_bin", "go2rtc")
		port := inst.Setting("go2rtc_port", "1984")

		if err := g2m.ensure(binPath, port); err != nil {
			return rtspError(err.Error())
		}

		go w.ensureStream(g2m.baseURL, inst.ID, streamURL)
		return rtspStream(inst.ID)
	}
}

// ServeFrame proxies the MJPEG stream from go2rtc to the browser.
// The browser keeps this connection open and displays live video natively via <img>.
func (w *RTSPGrabberWidget) ServeFrame(rw http.ResponseWriter, r *http.Request, inst widgets.WidgetInstance) {
	g2m.mu.Lock()
	running := g2m.running
	baseURL := g2m.baseURL
	g2m.mu.Unlock()

	if !running {
		http.Error(rw, "go2rtc no está corriendo", http.StatusServiceUnavailable)
		return
	}

	if src := inst.Setting("stream_url", ""); src != "" {
		w.ensureStream(baseURL, inst.ID, src)
	}

	mjpegURL := fmt.Sprintf("%s/api/stream.mjpeg?src=%s", baseURL, url.QueryEscape(inst.ID))
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, mjpegURL, nil)
	if err != nil {
		http.Error(rw, "internal error", http.StatusInternalServerError)
		return
	}

	resp, err := (&http.Client{Timeout: 0}).Do(req)
	if err != nil {
		http.Error(rw, "go2rtc unavailable: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	for k, vs := range resp.Header {
		for _, v := range vs {
			rw.Header().Add(k, v)
		}
	}
	rw.WriteHeader(resp.StatusCode)
	io.Copy(rw, resp.Body) //nolint:errcheck
}

func (w *RTSPGrabberWidget) ConfigSchema() []widgets.ConfigField {
	return []widgets.ConfigField{
		{
			Key:      "stream_type",
			Label:    "Tipo de stream",
			Type:     "select",
			Required: true,
			Default:  "rtsp",
			Options:  []string{"rtsp", "http"},
		},
		{
			Key:         "stream_url",
			Label:       "URL del stream",
			Type:        "text",
			Required:    true,
			Placeholder: "rtsp://user:pass@192.168.1.10:554/stream1",
		},
		{
			Key:         "go2rtc_bin",
			Label:       "Binario go2rtc (solo modo RTSP)",
			Type:        "text",
			Default:     "go2rtc",
			Placeholder: "go2rtc  o  /usr/local/bin/go2rtc",
		},
		{
			Key:         "go2rtc_port",
			Label:       "Puerto go2rtc (solo modo RTSP)",
			Type:        "number",
			Default:     "1984",
			Placeholder: "1984",
		},
	}
}

// ensureStream registers the RTSP source in go2rtc once per widget instance.
// Uses PUT /api/streams?name=&src= — idempotent in go2rtc.
func (w *RTSPGrabberWidget) ensureStream(baseURL, name, src string) {
	key := baseURL + "|" + name
	if _, ok := w.registered.Load(key); ok {
		return
	}

	apiURL := fmt.Sprintf("%s/api/streams?name=%s&src=%s",
		baseURL,
		url.QueryEscape(name),
		url.QueryEscape(src),
	)
	req, err := http.NewRequest(http.MethodPut, apiURL, nil)
	if err != nil {
		return
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()

	if resp.StatusCode < 300 {
		w.registered.Store(key, struct{}{})
	}
}
