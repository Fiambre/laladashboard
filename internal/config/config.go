package config

import "github.com/rfguerreroa/laladashboard/internal/widgets"

type DashboardConfig struct {
	Version string                   `json:"version"`
	Theme   string                   `json:"theme"`
	Columns int                      `json:"columns"`
	Widgets []widgets.WidgetInstance `json:"widgets"`
}

type WidgetLayout struct {
	ID string `json:"id"`
	X  int    `json:"x"`
	Y  int    `json:"y"`
	W  int    `json:"w"`
	H  int    `json:"h"`
}

func DefaultConfig() DashboardConfig {
	return DashboardConfig{
		Version: "1",
		Theme:   "dark",
		Columns: 12,
		Widgets: []widgets.WidgetInstance{
			{
				ID:      "clock-default",
				TypeID:  "clock",
				Title:   "Reloj",
				Enabled: true,
				Settings: map[string]string{
					"timezone":     "America/Santiago",
					"format":       "24h",
					"show_seconds": "true",
					"show_date":    "true",
				},
				Grid: widgets.GridPosition{X: 0, Y: 0, W: 3, H: 2},
			},
		},
	}
}
