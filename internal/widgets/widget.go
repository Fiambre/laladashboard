package widgets

import (
	"context"

	"github.com/a-h/templ"
)

type Widget interface {
	TypeID() string
	DisplayName() string
	Render(ctx context.Context, inst WidgetInstance) templ.Component
	RenderContent(ctx context.Context, inst WidgetInstance) templ.Component
	ConfigSchema() []ConfigField
}

type WidgetInstance struct {
	ID       string            `json:"id"`
	TypeID   string            `json:"type_id"`
	Title    string            `json:"title"`
	Enabled  bool              `json:"enabled"`
	Settings map[string]string `json:"settings"`
	Grid     GridPosition      `json:"grid"`
}

type GridPosition struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

type ConfigField struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Type        string   `json:"type"`
	Default     string   `json:"default"`
	Required    bool     `json:"required"`
	Options     []string `json:"options,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
}

func (i WidgetInstance) Setting(key, fallback string) string {
	if v, ok := i.Settings[key]; ok && v != "" {
		return v
	}
	return fallback
}
