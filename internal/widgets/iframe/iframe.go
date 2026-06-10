package iframe

import (
	"context"

	"github.com/a-h/templ"
	"github.com/rfguerreroa/laladashboard/internal/registry"
	"github.com/rfguerreroa/laladashboard/internal/widgets"
)

func init() {
	registry.Register(&IframeWidget{})
}

type IframeWidget struct{}

func (f *IframeWidget) TypeID() string      { return "iframe" }
func (f *IframeWidget) DisplayName() string { return "Iframe / Web" }

func (f *IframeWidget) Render(ctx context.Context, inst widgets.WidgetInstance) templ.Component {
	return f.RenderContent(ctx, inst)
}

func (f *IframeWidget) RenderContent(ctx context.Context, inst widgets.WidgetInstance) templ.Component {
	url := inst.Setting("url", "")
	if url == "" {
		return iframeError("Configura la URL a embeber")
	}
	return iframeContent(url)
}

func (f *IframeWidget) ConfigSchema() []widgets.ConfigField {
	return []widgets.ConfigField{
		{Key: "url", Label: "URL", Type: "url", Required: true, Placeholder: "https://example.com"},
		{Key: "poll_seconds", Label: "Recargar cada (seg, 0=nunca)", Type: "number", Default: "0"},
	}
}
