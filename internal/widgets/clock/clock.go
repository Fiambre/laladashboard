package clock

import (
	"context"

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

func (c *ClockWidget) RenderContent(_ context.Context, inst widgets.WidgetInstance) templ.Component {
	tz := inst.Setting("timezone", "America/Santiago")
	format := inst.Setting("format", "24h")
	showSeconds := inst.Setting("show_seconds", "true") == "true"
	showDate := inst.Setting("show_date", "true") == "true"
	return clockContent(tz, format, showSeconds, showDate)
}

func (c *ClockWidget) ConfigSchema() []widgets.ConfigField {
	return []widgets.ConfigField{
		{Key: "timezone", Label: "Zona horaria", Type: "text", Default: "America/Santiago", Placeholder: "America/Santiago"},
		{Key: "format", Label: "Formato", Type: "select", Default: "24h", Options: []string{"24h", "12h"}},
		{Key: "show_seconds", Label: "Mostrar segundos", Type: "select", Default: "true", Options: []string{"true", "false"}},
		{Key: "show_date", Label: "Mostrar fecha", Type: "select", Default: "true", Options: []string{"true", "false"}},
	}
}
