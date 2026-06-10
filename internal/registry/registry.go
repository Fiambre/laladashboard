package registry

import (
	"fmt"
	"sync"

	"github.com/rfguerreroa/laladashboard/internal/widgets"
)

type Registry struct {
	mu      sync.RWMutex
	widgets map[string]widgets.Widget
}

var global = &Registry{
	widgets: make(map[string]widgets.Widget),
}

func Global() *Registry { return global }

func Register(w widgets.Widget) {
	global.mu.Lock()
	defer global.mu.Unlock()
	if _, exists := global.widgets[w.TypeID()]; exists {
		panic(fmt.Sprintf("widget type already registered: %s", w.TypeID()))
	}
	global.widgets[w.TypeID()] = w
}

func (r *Registry) Get(typeID string) (widgets.Widget, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	w, ok := r.widgets[typeID]
	return w, ok
}

func (r *Registry) All() []widgets.Widget {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]widgets.Widget, 0, len(r.widgets))
	for _, w := range r.widgets {
		result = append(result, w)
	}
	return result
}
