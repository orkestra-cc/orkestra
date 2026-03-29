package services

import (
	"sync"
	"time"

	"github.com/orkestra/backend/internal/sales/models"
)

// SkillTask represents an in-flight or completed skill execution
type SkillTask struct {
	ID        string                     `json:"id"`
	Skill     string                     `json:"skill"`
	Status    string                     `json:"status"` // "running", "completed", "failed"
	Result    *models.SkillResultInternal `json:"result,omitempty"`
	Error     string                     `json:"error,omitempty"`
	CreatedAt time.Time                  `json:"createdAt"`
}

// SkillStore is an in-memory store for async skill results with automatic TTL cleanup.
type SkillStore struct {
	mu    sync.RWMutex
	tasks map[string]*SkillTask
}

// NewSkillStore creates a new SkillStore and starts a background cleanup goroutine.
func NewSkillStore() *SkillStore {
	s := &SkillStore{tasks: make(map[string]*SkillTask)}
	go s.cleanup()
	return s
}

// Put stores or updates a skill task.
func (s *SkillStore) Put(task *SkillTask) {
	s.mu.Lock()
	s.tasks[task.ID] = task
	s.mu.Unlock()
}

// Get retrieves a skill task by ID. Returns nil if not found.
func (s *SkillStore) Get(id string) *SkillTask {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tasks[id]
}

// cleanup removes completed/failed tasks older than 10 minutes every 60 seconds.
func (s *SkillStore) cleanup() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		cutoff := time.Now().Add(-10 * time.Minute)
		for id, t := range s.tasks {
			if t.Status != "running" && t.CreatedAt.Before(cutoff) {
				delete(s.tasks, id)
			}
		}
		s.mu.Unlock()
	}
}
