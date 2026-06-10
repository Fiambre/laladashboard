package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/a-h/templ"
	"github.com/rfguerreroa/laladashboard/internal/registry"
	"github.com/rfguerreroa/laladashboard/internal/widgets"
)

func init() {
	registry.Register(&WeatherWidget{})
}

type WeatherWidget struct {
	mu    sync.Mutex
	cache map[string]cachedWeather
}

type cachedWeather struct {
	data      WeatherData
	fetchedAt time.Time
}

type ForecastDay struct {
	Day  string
	Icon string
	High string
	Low  string
}

type WeatherData struct {
	TempStr      string
	FeelsLikeStr string
	HumidityStr  string
	WindStr      string
	WindIcon     string
	Description  string
	City         string
	Icon         string
	ForecastTitle string
	Forecast     []ForecastDay
}

type owmCurrentResponse struct {
	Name string `json:"name"`
	Sys  struct {
		Country string `json:"country"`
	} `json:"sys"`
	Main struct {
		Temp      float64 `json:"temp"`
		FeelsLike float64 `json:"feels_like"`
		Humidity  int     `json:"humidity"`
	} `json:"main"`
	Wind struct {
		Speed float64 `json:"speed"`
		Deg   float64 `json:"deg"`
	} `json:"wind"`
	Weather []struct {
		Description string `json:"description"`
		ID          int    `json:"id"`
	} `json:"weather"`
}

type owmForecastResponse struct {
	City struct {
		Name    string `json:"name"`
		Country string `json:"country"`
	} `json:"city"`
	List []struct {
		Dt   int64 `json:"dt"`
		Main struct {
			TempMax float64 `json:"temp_max"`
			TempMin float64 `json:"temp_min"`
		} `json:"main"`
		Weather []struct {
			ID int `json:"id"`
		} `json:"weather"`
	} `json:"list"`
}

func (w *WeatherWidget) TypeID() string      { return "weather" }
func (w *WeatherWidget) DisplayName() string { return "Clima" }

func (w *WeatherWidget) Render(ctx context.Context, inst widgets.WidgetInstance) templ.Component {
	return w.RenderContent(ctx, inst)
}

func (w *WeatherWidget) RenderContent(ctx context.Context, inst widgets.WidgetInstance) templ.Component {
	apiKey := inst.Setting("api_key", "")
	if apiKey == "" {
		return weatherError("Configura tu API key de OpenWeatherMap")
	}

	pollSecs := 300
	fmt.Sscanf(inst.Setting("poll_seconds", "300"), "%d", &pollSecs)
	ttl := time.Duration(pollSecs) * time.Second

	w.mu.Lock()
	if w.cache == nil {
		w.cache = make(map[string]cachedWeather)
	}
	cached, ok := w.cache[inst.ID]
	w.mu.Unlock()

	if ok && time.Since(cached.fetchedAt) < ttl {
		return weatherContent(cached.data)
	}

	city := inst.Setting("city", "Santiago,CL")
	units := inst.Setting("units", "metric")

	current, err := fetchCurrent(apiKey, city, units)
	if err != nil {
		if ok {
			return weatherContent(cached.data)
		}
		return weatherError("Error al obtener datos: " + err.Error())
	}

	forecast, _ := fetchForecast(apiKey, city, units)
	current.Forecast = forecast
	if current.City != "" && current.ForecastTitle == "" {
		current.ForecastTitle = fmt.Sprintf("WEATHER FORECAST %s", current.City)
	}

	w.mu.Lock()
	w.cache[inst.ID] = cachedWeather{data: current, fetchedAt: time.Now()}
	w.mu.Unlock()

	return weatherContent(current)
}

func (w *WeatherWidget) ConfigSchema() []widgets.ConfigField {
	return []widgets.ConfigField{
		{Key: "api_key", Label: "API Key OpenWeatherMap", Type: "text", Required: true, Placeholder: "tu_api_key"},
		{Key: "city", Label: "Ciudad", Type: "text", Default: "Santiago,CL", Placeholder: "Santiago,CL"},
		{Key: "units", Label: "Unidades", Type: "select", Default: "metric", Options: []string{"metric", "imperial"}},
		{Key: "poll_seconds", Label: "Actualizar cada (seg)", Type: "number", Default: "300"},
	}
}

func fetchCurrent(apiKey, city, units string) (WeatherData, error) {
	url := fmt.Sprintf(
		"https://api.openweathermap.org/data/2.5/weather?q=%s&appid=%s&units=%s",
		city, apiKey, units,
	)
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return WeatherData{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return WeatherData{}, fmt.Errorf("API error %d", resp.StatusCode)
	}

	var owm owmCurrentResponse
	if err := json.NewDecoder(resp.Body).Decode(&owm); err != nil {
		return WeatherData{}, err
	}

	icon, desc := "🌡", ""
	if len(owm.Weather) > 0 {
		icon = weatherIcon(owm.Weather[0].ID)
		desc = owm.Weather[0].Description
	}
	_ = desc

	location := owm.Name
	if owm.Sys.Country != "" {
		location = fmt.Sprintf("%s, %s-%s", owm.Name, owm.Sys.Country, owm.Sys.Country)
	}

	return WeatherData{
		TempStr:      fmt.Sprintf("%.1f", owm.Main.Temp),
		FeelsLikeStr: fmt.Sprintf("%.1f", owm.Main.FeelsLike),
		HumidityStr:  fmt.Sprintf("%d", owm.Main.Humidity),
		WindStr:      fmt.Sprintf("%.0f", owm.Wind.Speed),
		WindIcon:     windDirectionIcon(owm.Wind.Deg),
		Icon:         icon,
		City:         location,
		ForecastTitle: fmt.Sprintf("WEATHER FORECAST %s, %s-%s", owm.Name, owm.Sys.Country, owm.Sys.Country),
	}, nil
}

func fetchForecast(apiKey, city, units string) ([]ForecastDay, error) {
	url := fmt.Sprintf(
		"https://api.openweathermap.org/data/2.5/forecast?q=%s&appid=%s&units=%s&cnt=40",
		city, apiKey, units,
	)
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("forecast API error %d", resp.StatusCode)
	}

	var owm owmForecastResponse
	if err := json.NewDecoder(resp.Body).Decode(&owm); err != nil {
		return nil, err
	}

	// Group by day, pick min/max
	type dayData struct {
		min, max float64
		icon     string
		set      bool
	}
	days := make(map[string]*dayData)
	var dayOrder []string

	for _, item := range owm.List {
		t := time.Unix(item.Dt, 0)
		key := t.Format("Mon")
		today := time.Now().Format("Mon")
		if key == today {
			continue
		}
		if _, exists := days[key]; !exists {
			days[key] = &dayData{min: 9999, max: -9999}
			dayOrder = append(dayOrder, key)
		}
		d := days[key]
		if item.Main.TempMax > d.max {
			d.max = item.Main.TempMax
		}
		if item.Main.TempMin < d.min {
			d.min = item.Main.TempMin
		}
		if !d.set && len(item.Weather) > 0 {
			d.icon = weatherIcon(item.Weather[0].ID)
			d.set = true
		}
	}

	var forecast []ForecastDay
	for i, day := range dayOrder {
		if i >= 5 {
			break
		}
		d := days[day]
		forecast = append(forecast, ForecastDay{
			Day:  day,
			Icon: d.icon,
			High: fmt.Sprintf("%.1f", d.max),
			Low:  fmt.Sprintf("%.1f", d.min),
		})
	}
	return forecast, nil
}

func weatherIcon(id int) string {
	switch {
	case id >= 200 && id < 300:
		return "⛈"
	case id >= 300 && id < 400:
		return "🌦"
	case id >= 500 && id < 600:
		return "🌧"
	case id >= 600 && id < 700:
		return "❄"
	case id >= 700 && id < 800:
		return "🌫"
	case id == 800:
		return "☀"
	case id == 801:
		return "🌤"
	case id >= 802:
		return "☁"
	default:
		return "🌡"
	}
}

func windDirectionIcon(deg float64) string {
	dirs := []string{"N", "NE", "E", "SE", "S", "SW", "W", "NW"}
	idx := int((deg+22.5)/45) % 8
	return "➤ " + dirs[idx]
}
