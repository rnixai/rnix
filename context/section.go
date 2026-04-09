package context

import (
	"log"
	"strings"
	"sync"
)

// PromptSection represents a named section of the system prompt.
// Each section has a compute function that generates its content.
// Cached sections compute once and reuse until Invalidate() is called.
type PromptSection struct {
	Name      string
	ComputeFn func() string
	Cached    bool
	value     string
	computed  bool
}

// SectionRegistry holds an ordered list of prompt sections and assembles them into
// a complete system prompt. Thread-safe via sync.Mutex.
type SectionRegistry struct {
	mu       sync.Mutex
	sections []*PromptSection
}

// NewSectionRegistry creates a new empty SectionRegistry.
func NewSectionRegistry() *SectionRegistry {
	return &SectionRegistry{}
}

// Register adds a named section to the registry. Sections are assembled in registration order.
func (r *SectionRegistry) Register(name string, fn func() string, cached bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sections = append(r.sections, &PromptSection{
		Name:      name,
		ComputeFn: fn,
		Cached:    cached,
	})
}

// Build assembles all sections into a single system prompt string.
// Cached sections reuse their previously computed value; non-cached sections
// recompute on every call. Empty section results are skipped.
// If a section's ComputeFn panics, the panic is recovered and the section is skipped.
//
// Lock ordering: Build() releases r.mu before calling ComputeFn to avoid holding
// SectionRegistry.mu while ComputeFn acquires proc.mu or other locks.
func (r *SectionRegistry) Build() string {
	// Phase 1: snapshot sections and identify which need computation
	r.mu.Lock()
	type sectionWork struct {
		section    *PromptSection
		needsWork  bool
		cachedVal  string
	}
	work := make([]sectionWork, len(r.sections))
	for i, s := range r.sections {
		if s.Cached && s.computed {
			work[i] = sectionWork{section: s, needsWork: false, cachedVal: s.value}
		} else {
			work[i] = sectionWork{section: s, needsWork: true}
		}
	}
	r.mu.Unlock()

	// Phase 2: compute values WITHOUT holding the lock
	values := make([]string, len(work))
	for i, w := range work {
		if !w.needsWork {
			values[i] = w.cachedVal
			continue
		}
		values[i] = r.computeSection(w.section)
	}

	// Phase 3: store cached results
	r.mu.Lock()
	for i, w := range work {
		if w.needsWork && w.section.Cached {
			w.section.value = values[i]
			w.section.computed = true
		}
	}
	r.mu.Unlock()

	// Phase 4: assemble
	var parts []string
	for _, val := range values {
		if val != "" {
			parts = append(parts, val)
		}
	}

	if len(parts) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString(parts[0])
	for i := 1; i < len(parts); i++ {
		b.WriteString("\n\n")
		b.WriteString(parts[i])
	}
	return b.String()
}

// computeSection evaluates a section's ComputeFn with panic recovery.
func (r *SectionRegistry) computeSection(s *PromptSection) (result string) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("[section] panic in ComputeFn for section %q: %v", s.Name, rec)
			result = ""
		}
	}()
	return s.ComputeFn()
}

// Invalidate resets all cached sections so they recompute on the next Build().
func (r *SectionRegistry) Invalidate() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.sections {
		if s.Cached {
			s.value = ""
			s.computed = false
		}
	}
}

// Len returns the number of registered sections.
func (r *SectionRegistry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sections)
}
