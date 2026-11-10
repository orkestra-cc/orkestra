// Package exporttest holds in-memory fakes for the stored DSR exports: a
// repository and an object store with a download presigner. Never used in
// production.
package exporttest

import (
	"context"
	"errors"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// MemRepo keeps export rows in memory.
type MemRepo struct {
	mu   sync.Mutex
	Rows map[string]models.DSRExport
}

func NewMemRepo() *MemRepo { return &MemRepo{Rows: map[string]models.DSRExport{}} }

func (m *MemRepo) Insert(_ context.Context, e *models.DSRExport) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Rows[e.UUID] = *e
	return nil
}

func (m *MemRepo) Get(_ context.Context, id string) (*models.DSRExport, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.Rows[id]
	if !ok {
		return nil, repository.ErrDSRExportNotFound
	}
	return &e, nil
}

func (m *MemRepo) ListByUser(_ context.Context, userUUID string) ([]models.DSRExport, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []models.DSRExport
	for _, e := range m.Rows {
		if e.UserUUID == userUUID {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (m *MemRepo) ListExpired(_ context.Context, now time.Time, limit int) ([]models.DSRExport, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []models.DSRExport
	for _, e := range m.Rows {
		if !e.ExpiresAt.After(now) {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ExpiresAt.Before(out[j].ExpiresAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *MemRepo) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.Rows, id)
	return nil
}

// Presign records one presign call.
type Presign struct {
	Key, DownloadAs string
	TTL             time.Duration
}

// MemStore is an in-memory iface.ObjectStore that also implements
// blob.ObjectDownloadPresigner.
type MemStore struct {
	mu           sync.Mutex
	Objects      map[string][]byte
	ContentTypes map[string]string
	Presigns     []Presign
	DeleteErr    error
}

func NewMemStore() *MemStore {
	return &MemStore{Objects: map[string][]byte{}, ContentTypes: map[string]string{}}
}

func (s *MemStore) Put(_ context.Context, key, contentType string, body io.Reader) error {
	b, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Objects[key] = b
	s.ContentTypes[key] = contentType
	return nil
}

func (s *MemStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.DeleteErr != nil {
		return s.DeleteErr
	}
	delete(s.Objects, key)
	return nil
}

func (s *MemStore) Exists(_ context.Context, key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.Objects[key]
	return ok, nil
}

func (s *MemStore) PresignGet(_ context.Context, key string, ttl time.Duration) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Presigns = append(s.Presigns, Presign{Key: key, TTL: ttl})
	return "https://store.test/get/" + key, nil
}

func (s *MemStore) PresignGetDownload(_ context.Context, key, downloadAs string, ttl time.Duration) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Presigns = append(s.Presigns, Presign{Key: key, DownloadAs: downloadAs, TTL: ttl})
	return "https://store.test/download/" + key, nil
}

// Err is a storage failure for tests.
var Err = errors.New("exporttest: storage failure")

func (s *MemStore) PresignPut(_ context.Context, key, _ string, _ int64, _ time.Duration) (*iface.PresignedPut, error) {
	return &iface.PresignedPut{URL: "https://store.test/put/" + key}, nil
}
