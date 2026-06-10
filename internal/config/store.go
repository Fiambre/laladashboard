package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/rfguerreroa/laladashboard/internal/widgets"
)

type Store struct {
	mu       sync.RWMutex
	filePath string
	current  DashboardConfig
}

func NewStore(filePath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return nil, err
	}
	s := &Store{filePath: filePath}
	if err := s.Load(); err != nil {
		s.current = DefaultConfig()
		_ = s.Save()
	}
	return s, nil
}

func (s *Store) Load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return json.Unmarshal(data, &s.current)
}

func (s *Store) Save() error {
	s.mu.RLock()
	data, err := json.MarshalIndent(s.current, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	tmp := s.filePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	// On Windows, Rename fails if destination exists — remove first
	_ = os.Remove(s.filePath)
	return os.Rename(tmp, s.filePath)
}

func (s *Store) Get() DashboardConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cfg := s.current
	// Return a deep copy of widgets slice
	cfg.Widgets = make([]widgets.WidgetInstance, len(s.current.Widgets))
	copy(cfg.Widgets, s.current.Widgets)
	return cfg
}

func (s *Store) UpdateLayout(positions []WidgetLayout) error {
	s.mu.Lock()
	for _, pos := range positions {
		for i, w := range s.current.Widgets {
			if w.ID == pos.ID {
				s.current.Widgets[i].Grid = widgets.GridPosition{
					X: pos.X, Y: pos.Y, W: pos.W, H: pos.H,
				}
				break
			}
		}
	}
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) UpdateSettings(widgetID string, settings map[string]string) error {
	s.mu.Lock()
	found := false
	for i, w := range s.current.Widgets {
		if w.ID == widgetID {
			s.current.Widgets[i].Settings = settings
			found = true
			break
		}
	}
	s.mu.Unlock()
	if !found {
		return fmt.Errorf("widget %s not found", widgetID)
	}
	return s.Save()
}

func (s *Store) AddWidget(inst widgets.WidgetInstance) error {
	s.mu.Lock()
	s.current.Widgets = append(s.current.Widgets, inst)
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) RemoveWidget(widgetID string) error {
	s.mu.Lock()
	filtered := s.current.Widgets[:0]
	for _, w := range s.current.Widgets {
		if w.ID != widgetID {
			filtered = append(filtered, w)
		}
	}
	s.current.Widgets = filtered
	s.mu.Unlock()
	return s.Save()
}

func (s *Store) SetTheme(theme string) error {
	s.mu.Lock()
	s.current.Theme = theme
	s.mu.Unlock()
	return s.Save()
}
