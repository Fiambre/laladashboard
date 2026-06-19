package uptimechart

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/a-h/templ"
	"github.com/rfguerreroa/laladashboard/internal/registry"
	"github.com/rfguerreroa/laladashboard/internal/widgets"
)

func init() {
	registry.Register(&UptimeChartWidget{})
}

type UptimeChartWidget struct {
	workers sync.Map
}

type slot struct {
	dayStart time.Time
	total    int
	up       int
}

// persistedSlot is the on-disk representation of one day's data.
type persistedSlot struct {
	Date  string `json:"d"`
	Total int    `json:"t"`
	Up    int    `json:"u"`
}

type uptimeWorker struct {
	mu      sync.RWMutex
	slots   []slot
	days    int
	dataDir string
	active  bool
	cancel  context.CancelFunc
}

func (w *UptimeChartWidget) TypeID() string      { return "uptime-chart" }
func (w *UptimeChartWidget) DisplayName() string { return "Uptime Chart" }

func (w *UptimeChartWidget) Render(ctx context.Context, inst widgets.WidgetInstance) templ.Component {
	return w.RenderContent(ctx, inst)
}

func (w *UptimeChartWidget) RenderContent(ctx context.Context, inst widgets.WidgetInstance) templ.Component {
	url := inst.Setting("url", "")
	if url == "" {
		return uptimeError("Configura la URL a monitorear")
	}

	label := inst.Setting("label", url)
	days := 60
	if n, err := strconv.Atoi(inst.Setting("days", "60")); err == nil && n > 0 && n <= 365 {
		days = n
	}
	intervalSec := 300
	if n, err := strconv.Atoi(inst.Setting("check_interval_seconds", "300")); err == nil && n >= 30 {
		intervalSec = n
	}

	// If the outer dashboard wrapper has no poll_seconds, the widget
	// content includes its own HTMX refresh so data stays current.
	selfPollSec := 0
	if p := inst.Setting("poll_seconds", ""); p == "" || p == "0" {
		selfPollSec = intervalSec
	}

	worker := w.touchWorker(inst.ID, url, days, intervalSec, inst.DataDir)

	worker.mu.RLock()
	slots := make([]slot, len(worker.slots))
	copy(slots, worker.slots)
	worker.mu.RUnlock()

	bars, uptimePct := buildBars(slots, days)
	streak := calcStreak(slots)
	return uptimeContent(label, bars, uptimePct, streak, days, inst.ID, selfPollSec)
}

func (w *UptimeChartWidget) ConfigSchema() []widgets.ConfigField {
	return []widgets.ConfigField{
		{Key: "url", Label: "URL", Type: "text", Required: true, Placeholder: "https://example.com"},
		{Key: "label", Label: "Etiqueta", Type: "text", Placeholder: "Mi Servicio"},
		{Key: "days", Label: "Días a mostrar", Type: "number", Default: "60"},
		{Key: "check_interval_seconds", Label: "Intervalo de chequeo (seg)", Type: "number", Default: "300"},
		{Key: "poll_seconds", Label: "Refresco UI (seg)", Type: "number", Default: "300"},
	}
}

func (w *UptimeChartWidget) touchWorker(instID, url string, days, intervalSec int, dataDir string) *uptimeWorker {
	val, _ := w.workers.LoadOrStore(instID, &uptimeWorker{days: days, dataDir: dataDir})
	worker := val.(*uptimeWorker)

	worker.mu.Lock()
	if !worker.active {
		worker.active = true
		worker.loadFromDisk()
		ctx, cancel := context.WithCancel(context.Background())
		worker.cancel = cancel
		worker.mu.Unlock()
		go worker.run(ctx, url, days, intervalSec)
		return worker
	}
	worker.mu.Unlock()
	return worker
}

func todayStart() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}

func (fw *uptimeWorker) currentSlot() *slot {
	today := todayStart()
	if len(fw.slots) == 0 || !fw.slots[len(fw.slots)-1].dayStart.Equal(today) {
		fw.slots = append(fw.slots, slot{dayStart: today})
		if len(fw.slots) > fw.days {
			fw.slots = fw.slots[len(fw.slots)-fw.days:]
		}
	}
	return &fw.slots[len(fw.slots)-1]
}

func (fw *uptimeWorker) run(ctx context.Context, url string, _ int, intervalSec int) {
	defer func() {
		fw.mu.Lock()
		fw.active = false
		fw.mu.Unlock()
	}()

	fw.doCheck(ctx, url)

	ticker := time.NewTicker(time.Duration(intervalSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fw.doCheck(ctx, url)
		}
	}
}

func (fw *uptimeWorker) doCheck(ctx context.Context, url string) {
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	up := false
	req, err := http.NewRequestWithContext(checkCtx, http.MethodHead, url, nil)
	if err == nil {
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
			up = resp.StatusCode < 500
		}
	}

	fw.mu.Lock()
	s := fw.currentSlot()
	s.total++
	if up {
		s.up++
	}
	snapshot := make([]slot, len(fw.slots))
	copy(snapshot, fw.slots)
	fw.mu.Unlock()

	fw.saveToDisk(snapshot)
}

// loadFromDisk restores slot history from the JSON file. Must be called under fw.mu.Lock().
func (fw *uptimeWorker) loadFromDisk() {
	if fw.dataDir == "" {
		return
	}
	data, err := os.ReadFile(filepath.Join(fw.dataDir, "slots.json"))
	if err != nil {
		return
	}
	var persisted []persistedSlot
	if err := json.Unmarshal(data, &persisted); err != nil {
		return
	}
	fw.slots = fw.slots[:0]
	for _, p := range persisted {
		d, err := time.ParseInLocation("2006-01-02", p.Date, time.Local)
		if err != nil {
			continue
		}
		fw.slots = append(fw.slots, slot{dayStart: d, total: p.Total, up: p.Up})
	}
	if len(fw.slots) > fw.days {
		fw.slots = fw.slots[len(fw.slots)-fw.days:]
	}
}

// saveToDisk writes a snapshot of slots to disk atomically. Safe to call without lock.
func (fw *uptimeWorker) saveToDisk(slots []slot) {
	if fw.dataDir == "" {
		return
	}
	persisted := make([]persistedSlot, len(slots))
	for i, s := range slots {
		persisted[i] = persistedSlot{
			Date:  s.dayStart.Format("2006-01-02"),
			Total: s.total,
			Up:    s.up,
		}
	}
	data, err := json.Marshal(persisted)
	if err != nil {
		return
	}
	path := filepath.Join(fw.dataDir, "slots.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return
	}
	os.Rename(tmp, path) //nolint:errcheck
}

type barInfo struct {
	Color string
	Title string
}

func buildBars(slots []slot, days int) ([]barInfo, string) {
	slotMap := make(map[string]slot, len(slots))
	for _, s := range slots {
		slotMap[s.dayStart.Format("2006-01-02")] = s
	}

	bars := make([]barInfo, days)
	totalUp, totalChecks := 0, 0

	for i := 0; i < days; i++ {
		day := todayStart().AddDate(0, 0, -(days - 1 - i))
		s, ok := slotMap[day.Format("2006-01-02")]

		var color, title string
		if !ok || s.total == 0 {
			color = "nodata"
			title = day.Format("02 Jan") + ": sin datos"
		} else {
			pct := float64(s.up) / float64(s.total) * 100
			totalUp += s.up
			totalChecks += s.total
			switch {
			case pct >= 95:
				color = "up"
			case pct >= 50:
				color = "degraded"
			default:
				color = "down"
			}
			title = fmt.Sprintf("%s: %.1f%% (%d/%d checks)", day.Format("02 Jan"), pct, s.up, s.total)
		}
		bars[i] = barInfo{Color: color, Title: title}
	}

	var uptimePct string
	switch {
	case totalChecks == 0:
		uptimePct = "—"
	case totalUp == totalChecks:
		uptimePct = "100%"
	default:
		uptimePct = fmt.Sprintf("%.2f%%", float64(totalUp)/float64(totalChecks)*100)
	}

	return bars, uptimePct
}

// calcStreak returns how many hours the service has been continuously green
// (≥95% uptime per day). Returns "" if there is no data.
func calcStreak(slots []slot) string {
	now := time.Now()

	// Find the most recent day that was not fully green.
	lastBadIdx := -1
	for i := len(slots) - 1; i >= 0; i-- {
		s := slots[i]
		if s.total == 0 {
			continue
		}
		if float64(s.up)/float64(s.total)*100 < 95 {
			lastBadIdx = i
			break
		}
	}

	var streakStart time.Time
	if lastBadIdx == -1 {
		// All recorded days are green — streak starts at oldest data point.
		for _, s := range slots {
			if s.total > 0 {
				streakStart = s.dayStart
				break
			}
		}
	} else {
		streakStart = slots[lastBadIdx].dayStart.AddDate(0, 0, 1)
	}

	if streakStart.IsZero() || streakStart.After(now) {
		return "0h"
	}
	return fmt.Sprintf("%dh", int(now.Sub(streakStart).Hours()))
}
