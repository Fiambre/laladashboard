package rss

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/a-h/templ"
	"github.com/mmcdole/gofeed"
	"github.com/rfguerreroa/laladashboard/internal/registry"
	"github.com/rfguerreroa/laladashboard/internal/widgets"
)

func init() {
	registry.Register(&RSSWidget{})
}

type RSSWidget struct {
	mu    sync.Mutex
	cache map[string]cachedFeed
}

type cachedFeed struct {
	title     string
	items     []*gofeed.Item
	fetchedAt time.Time
}

func (r *RSSWidget) TypeID() string      { return "rss" }
func (r *RSSWidget) DisplayName() string { return "RSS / Noticias" }

func (r *RSSWidget) Render(ctx context.Context, inst widgets.WidgetInstance) templ.Component {
	return r.RenderContent(ctx, inst)
}

func (r *RSSWidget) RenderContent(ctx context.Context, inst widgets.WidgetInstance) templ.Component {
	url := inst.Setting("url", "")
	if url == "" {
		return rssError("Configura la URL del feed RSS")
	}

	maxItems := 5
	fmt.Sscanf(inst.Setting("max_items", "5"), "%d", &maxItems)

	pollSecs := 900
	fmt.Sscanf(inst.Setting("poll_seconds", "900"), "%d", &pollSecs)
	ttl := time.Duration(pollSecs) * time.Second

	style := inst.Setting("style", "list")

	r.mu.Lock()
	if r.cache == nil {
		r.cache = make(map[string]cachedFeed)
	}
	cached, ok := r.cache[inst.ID]
	r.mu.Unlock()

	if ok && time.Since(cached.fetchedAt) < ttl {
		items := cached.items
		if len(items) > maxItems {
			items = items[:maxItems]
		}
		return rssContent(cached.title, items, style)
	}

	fp := gofeed.NewParser()
	feed, err := fp.ParseURLWithContext(url, ctx)
	if err != nil {
		if ok {
			items := cached.items
			if len(items) > maxItems {
				items = items[:maxItems]
			}
			return rssContent(cached.title, items, style)
		}
		return rssError("Error al cargar feed: " + err.Error())
	}

	r.mu.Lock()
	r.cache[inst.ID] = cachedFeed{
		title:     feed.Title,
		items:     feed.Items,
		fetchedAt: time.Now(),
	}
	r.mu.Unlock()

	items := feed.Items
	if len(items) > maxItems {
		items = items[:maxItems]
	}
	return rssContent(feed.Title, items, style)
}

func (r *RSSWidget) ConfigSchema() []widgets.ConfigField {
	return []widgets.ConfigField{
		{Key: "url", Label: "URL del feed RSS/Atom", Type: "url", Required: true, Placeholder: "https://example.com/feed.xml"},
		{Key: "max_items", Label: "Máximo de noticias", Type: "number", Default: "5"},
		{Key: "style", Label: "Estilo", Type: "select", Default: "list", Options: []string{"list", "ticker"}},
		{Key: "poll_seconds", Label: "Actualizar cada (seg)", Type: "number", Default: "900"},
	}
}

func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hours", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	}
}
