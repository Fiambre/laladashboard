package rtspgrabber

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	// net is used only for net.SplitHostPort

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

// ── go2rtc subprocess manager ─────────────────────────────────────────────────

type go2rtcManager struct {
	mu      sync.Mutex
	baseURL string
	running bool
}

var g2m = &go2rtcManager{}

func (m *go2rtcManager) ensure(binPath, apiPort string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return nil
	}

	base := "http://127.0.0.1:" + apiPort

	// Already running (e.g. started externally)
	if pingGo2rtc(base) {
		m.baseURL = base
		m.running = true
		return nil
	}

	cfgPath := filepath.Join(os.TempDir(), "lala-go2rtc.yaml")
	cfg := fmt.Sprintf("api:\n  listen: ':%s'\n  origin: '*'\nwebrtc:\n  candidates:\n    - stun:8555\n  ice_servers:\n    - urls: [stun:stun.l.google.com:19302]\n", apiPort)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0600); err != nil {
		return fmt.Errorf("go2rtc config: %w", err)
	}

	cmd := exec.Command(binPath, "-config", cfgPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("go2rtc start (%q): %w", binPath, err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if pingGo2rtc(base) {
			m.baseURL = base
			m.running = true
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	cmd.Process.Kill() //nolint:errcheck
	return fmt.Errorf("go2rtc did not respond within 5s (binary: %q)", binPath)
}

func pingGo2rtc(baseURL string) bool {
	c := &http.Client{Timeout: 400 * time.Millisecond}
	resp, err := c.Get(baseURL + "/api")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 500
}

// ── SDP patching ─────────────────────────────────────────────────────────────

func extractHostIP(host string) string {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		return host
	}
	return h
}

// injectHostCandidate adds a host ICE candidate with the actual host IP and
// the go2rtc WebRTC port to the SDP answer.
//
// go2rtc runs inside Docker and discovers its external IP via STUN, but the
// STUN-reported port is a random ephemeral NAT port (not the exposed port 8555).
// The browser cannot reach that random port. By injecting a host candidate with
// the request's host IP and the known exposed port, we give the browser a
// reachable endpoint for WebRTC media.
func injectHostCandidate(sdp, hostIP, port string) string {
	// Parse ice-ufrag so our candidate matches the session.
	ufrag := ""
	for _, line := range strings.Split(sdp, "\n") {
		clean := strings.TrimRight(line, "\r")
		if strings.HasPrefix(clean, "a=ice-ufrag:") {
			ufrag = strings.TrimPrefix(clean, "a=ice-ufrag:")
			break
		}
	}

	cand := fmt.Sprintf("a=candidate:1 1 udp 2130706431 %s %s typ host", hostIP, port)
	if ufrag != "" {
		cand += " ufrag " + ufrag
	}

	// Insert after the last existing candidate line.
	lines := strings.Split(sdp, "\n")
	lastCand := -1
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimRight(l, "\r"), "a=candidate:") {
			lastCand = i
		}
	}
	if lastCand >= 0 {
		out := make([]string, 0, len(lines)+1)
		out = append(out, lines[:lastCand+1]...)
		out = append(out, cand)
		out = append(out, lines[lastCand+1:]...)
		return strings.Join(out, "\n")
	}
	return sdp + "\n" + cand + "\n"
}

// ── Widget ────────────────────────────────────────────────────────────────────

type RTSPGrabberWidget struct {
	registered sync.Map // key: go2rtcBaseURL+"|"+name → struct{}
}

func (w *RTSPGrabberWidget) TypeID() string      { return "rtsp-grabber" }
func (w *RTSPGrabberWidget) DisplayName() string { return "Cámara RTSP / HTTP" }

func (w *RTSPGrabberWidget) Render(ctx context.Context, inst widgets.WidgetInstance) templ.Component {
	return w.RenderContent(ctx, inst)
}

func (w *RTSPGrabberWidget) RenderContent(_ context.Context, inst widgets.WidgetInstance) templ.Component {
	streamURL := inst.Setting("stream_url", inst.Setting("rtsp_url", ""))
	if streamURL == "" {
		return rtspError("Configura la URL del stream")
	}

	switch inst.Setting("stream_type", "rtsp") {
	case "http":
		return rtspDirect(streamURL, detectDirectKind(streamURL))
	default: // "rtsp"
		binPath := inst.Setting("go2rtc_bin", "go2rtc")
		apiPort := inst.Setting("go2rtc_port", "1984")
		if err := g2m.ensure(binPath, apiPort); err != nil {
			return rtspError(err.Error())
		}
		go w.ensureStream(g2m.baseURL, inst.ID, streamURL)
		return rtspStream(inst.ID)
	}
}

// ServeWebRTC proxies the WebRTC SDP exchange with go2rtc and rewrites
// Docker-internal ICE candidate IPs with the real host IP from r.Host.
func (w *RTSPGrabberWidget) ServeWebRTC(rw http.ResponseWriter, r *http.Request, inst widgets.WidgetInstance) {
	g2m.mu.Lock()
	running := g2m.running
	baseURL := g2m.baseURL
	g2m.mu.Unlock()

	if !running {
		http.Error(rw, "go2rtc no está corriendo", http.StatusServiceUnavailable)
		return
	}

	streamURL := inst.Setting("stream_url", inst.Setting("rtsp_url", ""))
	if streamURL != "" {
		w.ensureStream(baseURL, inst.ID, streamURL)
	}

	offerSDP, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(rw, "error leyendo SDP offer", http.StatusBadRequest)
		return
	}

	targetURL := baseURL + "/api/webrtc?src=" + url.QueryEscape(inst.ID)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, targetURL,
		strings.NewReader(string(offerSDP)))
	if err != nil {
		http.Error(rw, "internal error", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/sdp")

	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		http.Error(rw, "go2rtc unavailable: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	answerBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(rw, "error leyendo SDP answer", http.StatusInternalServerError)
		return
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		rw.WriteHeader(resp.StatusCode)
		rw.Write(answerBytes) //nolint:errcheck
		return
	}

	hostIP := extractHostIP(r.Host)
	answer := injectHostCandidate(string(answerBytes), hostIP, "8555")

	rw.Header().Set("Content-Type", "application/sdp")
	rw.WriteHeader(http.StatusOK)
	rw.Write([]byte(answer)) //nolint:errcheck
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
			Key:     "go2rtc_bin",
			Label:   "Binario go2rtc",
			Type:    "text",
			Default: "go2rtc",
		},
		{
			Key:     "go2rtc_port",
			Label:   "Puerto API go2rtc",
			Type:    "number",
			Default: "1984",
		},
	}
}

// ensureStream registers the RTSP stream with go2rtc so it can serve it via WebRTC.
// go2rtc handles H264 natively (no transcoding) when the client requests WebRTC.
func (w *RTSPGrabberWidget) ensureStream(baseURL, name, src string) {
	key := baseURL + "|" + name
	if _, ok := w.registered.Load(key); ok {
		return
	}
	apiURL := fmt.Sprintf("%s/api/streams?name=%s&src=%s",
		baseURL, url.QueryEscape(name), url.QueryEscape(src))
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
