package jobs

import (
	"fmt"
	"sort"
	"sync"
)

// Entry is one registered job.
type Entry struct {
	Key    string
	Module string
	Def    Definition // normalized
}

// Catalog holds the jobs modules contribute. The kernel creates one and
// publishes it as module.ServiceJobCatalog before any module Init.
type Catalog struct {
	mu      sync.RWMutex
	entries map[string]Entry
}

// NewCatalog returns an empty catalog.
func NewCatalog() *Catalog { return &Catalog{entries: map[string]Entry{}} }

// Register adds a module's jobs atomically: if any definition is invalid or
// duplicates another (in the call or already in the catalog), nothing from
// the module is registered. Keys are "<module>.<Name>".
func (c *Catalog) Register(module string, defs []Definition) error {
	if module == "" {
		return fmt.Errorf("jobs: module name is required")
	}
	staged := make([]Entry, 0, len(defs))
	seen := make(map[string]bool, len(defs))
	for _, d := range defs {
		d = d.Normalized()
		if err := d.Validate(); err != nil {
			return fmt.Errorf("module %s: %w", module, err)
		}
		key := module + "." + d.Name
		if seen[key] {
			return fmt.Errorf("jobs: duplicate job %q", key)
		}
		seen[key] = true
		staged = append(staged, Entry{Key: key, Module: module, Def: d})
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range staged {
		if _, dup := c.entries[e.Key]; dup {
			return fmt.Errorf("jobs: job %q already registered", e.Key)
		}
	}
	for _, e := range staged {
		c.entries[e.Key] = e
	}
	return nil
}

// List returns all entries sorted by key.
func (c *Catalog) List() []Entry {
	c.mu.RLock()
	out := make([]Entry, 0, len(c.entries))
	for _, e := range c.entries {
		out = append(out, e)
	}
	c.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Get returns the entry for key.
func (c *Catalog) Get(key string) (Entry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[key]
	return e, ok
}
