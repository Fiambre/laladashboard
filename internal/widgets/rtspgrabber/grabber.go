package rtspgrabber

import (
	"bytes"
	"context"
	"net/http"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/a-h/templ"
	"github.com/rfguerreroa/laladashboard/internal/registry"
	"github.com/rfguerreroa/laladashboard/internal/widgets"
)

func init() {
	registry.Register(&RTSPGrabberWidget{})
}

type RTSPGrabberWidget struct {
	workers sync.Map
}

type frameWorker struct {
	mu       sync.RWMutex
	frame    []byte
	lastUsed time.Time
	cancel   context.CancelFunc
	active   bool
}

func (w *RTSPGrabberWidget) TypeID() string      { return "rtsp-grabber" }
func (w *RTSPGrabberWidget) DisplayName() string { return "Cámara RTSP" }

func (w *RTSPGrabberWidget) Render(ctx context.Context, inst widgets.WidgetInstance) templ.Component {
	return w.RenderContent(ctx, inst)
}

func (w *RTSPGrabberWidget) RenderContent(ctx context.Context, inst widgets.WidgetInstance) templ.Component {
	rtspURL := inst.Setting("rtsp_url", "")
	if rtspURL == "" {
		return rtspError("Configura la URL RTSP")
	}

	refreshMs := 1000
	if n, err := strconv.Atoi(inst.Setting("refresh_ms", "1000")); err == nil && n >= 100 {
		refreshMs = n
	}

	w.touchWorker(inst.ID, rtspURL, refreshMs)

	return rtspContent(inst.ID, strconv.Itoa(refreshMs))
}

func (w *RTSPGrabberWidget) ServeFrame(rw http.ResponseWriter, r *http.Request, inst widgets.WidgetInstance) {
	val, ok := w.workers.Load(inst.ID)
	if !ok {
		http.Error(rw, "no frame available", http.StatusServiceUnavailable)
		return
	}
	worker := val.(*frameWorker)

	worker.mu.Lock()
	worker.lastUsed = time.Now()
	frame := worker.frame
	worker.mu.Unlock()

	if len(frame) == 0 {
		http.Error(rw, "no frame yet", http.StatusServiceUnavailable)
		return
	}

	rw.Header().Set("Content-Type", "image/jpeg")
	rw.Header().Set("Cache-Control", "no-store")
	rw.Write(frame) //nolint:errcheck
}

func (w *RTSPGrabberWidget) ConfigSchema() []widgets.ConfigField {
	return []widgets.ConfigField{
		{Key: "rtsp_url", Label: "URL RTSP", Type: "text", Required: true, Placeholder: "rtsp://user:pass@host:554/stream1"},
		{Key: "refresh_ms", Label: "Intervalo (ms)", Type: "number", Default: "1000", Placeholder: "1000"},
	}
}

func (w *RTSPGrabberWidget) touchWorker(instID, rtspURL string, refreshMs int) {
	val, _ := w.workers.LoadOrStore(instID, &frameWorker{lastUsed: time.Now()})
	worker := val.(*frameWorker)

	worker.mu.Lock()
	worker.lastUsed = time.Now()
	if !worker.active {
		worker.active = true
		ctx, cancel := context.WithCancel(context.Background())
		worker.cancel = cancel
		worker.mu.Unlock()
		go worker.run(ctx, rtspURL, refreshMs)
		return
	}
	worker.mu.Unlock()
}

func (fw *frameWorker) run(ctx context.Context, rtspURL string, refreshMs int) {
	defer func() {
		fw.mu.Lock()
		fw.active = false
		fw.mu.Unlock()
	}()

	if refreshMs < 100 {
		refreshMs = 1000
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		fw.mu.RLock()
		idle := time.Since(fw.lastUsed) > 60*time.Second
		fw.mu.RUnlock()
		if idle {
			return
		}

		start := time.Now()
		frame, err := captureFrame(ctx, rtspURL)
		if err == nil && len(frame) > 0 {
			fw.mu.Lock()
			fw.frame = frame
			fw.mu.Unlock()
		}

		// Wait remaining time so capture cycle matches refreshMs
		elapsed := time.Since(start)
		wait := time.Duration(refreshMs)*time.Millisecond - elapsed
		if wait > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}
	}
}

func captureFrame(ctx context.Context, rtspURL string) ([]byte, error) {
	captureCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(captureCtx, "ffmpeg",
		"-rtsp_transport", "tcp",
		"-i", rtspURL,
		"-vframes", "1",
		"-f", "image2",
		"-vcodec", "mjpeg",
		"-q:v", "5",
		"-loglevel", "error",
		"pipe:1",
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
