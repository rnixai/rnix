package kernel

import (
	"sync"
	"time"
)

// LineageEvent records a single differentiation step in a process's lineage.
type LineageEvent struct {
	Timestamp  time.Time `json:"timestamp"`
	Phase      string    `json:"phase"`       // "initial" | "progressive"
	Skills     []string  `json:"skills"`      // skills loaded in this step
	Trigger    string    `json:"trigger"`     // intent or reason that triggered this differentiation
	FromMemory bool      `json:"from_memory"` // true if reused from DiffMemory
}

// Lineage tracks the complete differentiation history for a process.
type Lineage struct {
	mu     sync.RWMutex
	events []LineageEvent
}

// NewLineage creates a new empty Lineage.
func NewLineage() *Lineage {
	return &Lineage{}
}

// Record appends a lineage event.
func (l *Lineage) Record(event LineageEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event)
}

// Events returns a copy of all lineage events in order.
func (l *Lineage) Events() []LineageEvent {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if len(l.events) == 0 {
		return []LineageEvent{}
	}
	result := make([]LineageEvent, len(l.events))
	copy(result, l.events)
	// Deep copy Skills slices to prevent caller mutation
	for i := range result {
		if result[i].Skills != nil {
			skills := make([]string, len(result[i].Skills))
			copy(skills, result[i].Skills)
			result[i].Skills = skills
		}
	}
	return result
}
