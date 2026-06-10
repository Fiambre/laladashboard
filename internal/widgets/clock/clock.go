package clock

import (
	"context"
	"time"

	"github.com/a-h/templ"
	"github.com/rfguerreroa/laladashboard/internal/registry"
	"github.com/rfguerreroa/laladashboard/internal/widgets"
)

func init() {
	registry.Register(&ClockWidget{})
}

type ClockWidget struct{}

func (c *ClockWidget) TypeID() string      { return "clock" }
func (c *ClockWidget) DisplayName() string { return "Reloj" }

func (c *ClockWidget) Render(ctx context.Context, inst widgets.WidgetInstance) templ.Component {
	return c.RenderContent(ctx, inst)
}

func (c *ClockWidget) RenderContent(ctx context.Context, inst widgets.WidgetInstance) templ.Component {
	tz := inst.Setting("timezone", "America/Santiago")
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)

	format := inst.Setting("format", "24h")
	showSeconds := inst.Setting("show_seconds", "true") == "true"
	showDate := inst.Setting("show_date", "true") == "true"

	var hhmm, ss string
	if format == "12h" {
		hhmm = now.Format("3:04")
		ss = now.Format("05 PM")
	} else {
		hhmm = now.Format("15:04")
		ss = now.Format("05")
	}

	dateStr := ""
	if showDate {
		dateStr = now.Format("Monday, January 2, 2006")
	}

	return clockContent(dateStr, hhmm, ss, showSeconds)
}

func (c *ClockWidget) ConfigSchema() []widgets.ConfigField {
	return []widgets.ConfigField{
		{Key: "timezone", Label: "Zona horaria", Type: "text", Default: "America/Santiago", Placeholder: "America/Santiago"},
		{Key: "format", Label: "Formato", Type: "select", Default: "24h", Options: []string{"24h", "12h"}},
		{Key: "show_seconds", Label: "Mostrar segundos", Type: "select", Default: "true", Options: []string{"true", "false"}},
		{Key: "show_date", Label: "Mostrar fecha", Type: "select", Default: "true", Options: []string{"true", "false"}},
		{Key: "poll_seconds", Label: "Actualizar cada (seg)", Type: "number", Default: "1"},
	}
}
