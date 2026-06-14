package rtspgrabber

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os/exec"
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

// ── Per-stream FFmpeg worker ──────────────────────────────────────────────────

type subscriber struct {
	ch chan []byte
}

type frameWorker struct {
	mu       sync.Mutex
	frame    []byte     // most recent JPEG
	subs     []*subscriber
	lastUsed time.Time
	cancel   context.CancelFunc
	running  bool
	srcURL   string
}

var (
	workersMu sync.Mutex
	workerMap = map[string]*frameWorker{}
)

func getWorker(srcURL string) *frameWorker {
	workersMu.Lock()
	fw, ok := workerMap[srcURL]
	if !ok {
		fw = &frameWorker{srcURL: srcURL, lastUsed: time.Now()}
		workerMap[srcURL] = fw
	}
	workersMu.Unlock() // release before locking fw.mu to avoid deadlock with run()

	fw.mu.Lock()
	fw.lastUsed = time.Now()
	if !fw.running {
		fw.running = true
		ctx, cancel := context.WithCancel(context.Background())
		fw.cancel = cancel
		go fw.run(ctx)
	}
	fw.mu.Unlock()
	return fw
}

func (fw *frameWorker) subscribe() *subscriber {
	sub := &subscriber{ch: make(chan []byte, 1)}
	fw.mu.Lock()
	// Send latest frame immediately so the subscriber doesn't wait for the next one.
	if fw.frame != nil {
		sub.ch <- fw.frame
	}
	fw.subs = append(fw.subs, sub)
	fw.mu.Unlock()
	return sub
}

func (fw *frameWorker) unsubscribe(sub *subscriber) {
	fw.mu.Lock()
	for i, s := range fw.subs {
		if s == sub {
			fw.subs = append(fw.subs[:i], fw.subs[i+1:]...)
			break
		}
	}
	fw.mu.Unlock()
}

func (fw *frameWorker) broadcast(frame []byte) {
	fw.mu.Lock()
	fw.frame = frame
	fw.lastUsed = time.Now()
	for _, s := range fw.subs {
		select {
		case s.ch <- frame:
		default:
			// Subscriber is slow — overwrite with latest frame.
			select {
			case <-s.ch:
			default:
			}
			s.ch <- frame
		}
	}
	fw.mu.Unlock()
}

const idleTimeout = 60 * time.Second
const ffmpegBackoff = 3 * time.Second

func (fw *frameWorker) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Stop if no subscribers and idle for a while.
		fw.mu.Lock()
		idle := len(fw.subs) == 0 && time.Since(fw.lastUsed) > idleTimeout
		fw.mu.Unlock()
		if idle {
			fw.mu.Lock()
			fw.running = false
			fw.mu.Unlock()
			workersMu.Lock()
			delete(workerMap, fw.srcURL)
			workersMu.Unlock()
			return
		}

		fw.runFFmpeg(ctx)

		select {
		case <-ctx.Done():
			return
		case <-time.After(ffmpegBackoff):
		}
	}
}

var soiMarker = []byte{0xFF, 0xD8}
var eoiMarker = []byte{0xFF, 0xD9}

func (fw *frameWorker) runFFmpeg(ctx context.Context) {
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-hide_banner", "-loglevel", "error",
		"-rtsp_transport", "tcp",
		"-i", fw.srcURL,
		"-f", "mjpeg",
		"-r", "2",
		"-q:v", "15",
		"pipe:1",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	if err := cmd.Start(); err != nil {
		return
	}
	fw.parseMJPEGStream(ctx, stdout)
	cmd.Wait() //nolint:errcheck
}

func (fw *frameWorker) parseMJPEGStream(ctx context.Context, r io.Reader) {
	buf := make([]byte, 0, 512*1024)
	tmp := make([]byte, 32*1024)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			for {
				soiIdx := bytes.Index(buf, soiMarker)
				if soiIdx < 0 {
					buf = buf[:0]
					break
				}
				if soiIdx > 0 {
					buf = buf[soiIdx:]
				}
				eoiIdx := bytes.Index(buf[2:], eoiMarker)
				if eoiIdx < 0 {
					if len(buf) > 8*1024*1024 {
						buf = buf[:0]
					}
					break
				}
				end := 2 + eoiIdx + 2
				frame := make([]byte, end)
				copy(frame, buf[:end])
				buf = buf[end:]
				fw.broadcast(frame)
			}
		}
		if err != nil {
			return
		}
	}
}

// ── Widget ────────────────────────────────────────────────────────────────────

type RTSPGrabberWidget struct{}

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
		getWorker(streamURL) // warm up the worker
		return rtspStream(inst.ID)
	}
}

// ServeFrame streams MJPEG frames from the persistent FFmpeg worker.
func (w *RTSPGrabberWidget) ServeFrame(rw http.ResponseWriter, r *http.Request, inst widgets.WidgetInstance) {
	streamURL := inst.Setting("stream_url", inst.Setting("rtsp_url", ""))
	if streamURL == "" {
		http.Error(rw, "stream_url no configurada", http.StatusServiceUnavailable)
		return
	}

	fw := getWorker(streamURL)
	sub := fw.subscribe()
	defer fw.unsubscribe(sub)

	rw.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")
	rw.Header().Set("Cache-Control", "no-cache")
	rw.WriteHeader(http.StatusOK)

	flusher, canFlush := rw.(http.Flusher)

	for {
		select {
		case <-r.Context().Done():
			return
		case frame, ok := <-sub.ch:
			if !ok {
				return
			}
			fmt.Fprintf(rw, "--frame\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", len(frame))
			if _, err := rw.Write(frame); err != nil {
				return
			}
			rw.Write([]byte("\r\n")) //nolint:errcheck
			if canFlush {
				flusher.Flush()
			}
		}
	}
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
	}
}
