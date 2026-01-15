package services

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/orkestra/backend/internal/reporting/models"
	"github.com/orkestra/backend/internal/reporting/repository"
)

// Common errors
var (
	ErrInvalidInput = errors.New("invalid input data")
)

// DeadlineService gestisce la logica di business per le scadenze
type DeadlineService interface {
	GetAllDeadlines(ctx context.Context, filters models.DeadlineFilters, pagination models.PaginationParams) (*models.DeadlineReportResponse, error)
}

type deadlineService struct {
	deadlineRepo repository.DeadlineRepository
}

// NewDeadlineService crea una nuova istanza di DeadlineService
func NewDeadlineService(deadlineRepo repository.DeadlineRepository) DeadlineService {
	return &deadlineService{
		deadlineRepo: deadlineRepo,
	}
}

// GetAllDeadlines recupera tutte le scadenze con filtri e paginazione
func (s *deadlineService) GetAllDeadlines(ctx context.Context, filters models.DeadlineFilters, pagination models.PaginationParams) (*models.DeadlineReportResponse, error) {
	// Validate pagination params
	if pagination.Page < 1 {
		pagination.Page = 1
	}
	if pagination.PageSize < 1 || pagination.PageSize > 100 {
		pagination.PageSize = 20
	}

	// Recupera tutte le scadenze dai veicoli
	vehicleDeadlines, err := s.deadlineRepo.GetVehicleDeadlines(ctx)
	if err != nil {
		return nil, err
	}

	// Recupera tutte le scadenze dagli utenti
	userDeadlines, err := s.deadlineRepo.GetUserDeadlines(ctx)
	if err != nil {
		return nil, err
	}

	// Combina tutte le scadenze
	allDeadlines := append(vehicleDeadlines, userDeadlines...)

	// Applica i filtri
	filteredDeadlines := s.applyFilters(allDeadlines, filters)

	// Ordina per data di scadenza (le più vicine prima)
	sort.Slice(filteredDeadlines, func(i, j int) bool {
		return filteredDeadlines[i].ExpiryDate.Before(filteredDeadlines[j].ExpiryDate)
	})

	// Calcola la paginazione
	total := int64(len(filteredDeadlines))
	totalPages := int(total) / pagination.PageSize
	if int(total)%pagination.PageSize > 0 {
		totalPages++
	}

	// Applica la paginazione
	start := (pagination.Page - 1) * pagination.PageSize
	end := start + pagination.PageSize

	if start >= len(filteredDeadlines) {
		filteredDeadlines = []models.DeadlineItem{}
	} else {
		if end > len(filteredDeadlines) {
			end = len(filteredDeadlines)
		}
		filteredDeadlines = filteredDeadlines[start:end]
	}

	return &models.DeadlineReportResponse{
		Deadlines:  filteredDeadlines,
		Total:      total,
		Page:       pagination.Page,
		PageSize:   pagination.PageSize,
		TotalPages: totalPages,
	}, nil
}

// applyFilters applica i filtri alle scadenze
func (s *deadlineService) applyFilters(deadlines []models.DeadlineItem, filters models.DeadlineFilters) []models.DeadlineItem {
	var filtered []models.DeadlineItem

	for _, deadline := range deadlines {
		// Filtro per tipo di entità
		if filters.EntityType != "" && deadline.EntityType != filters.EntityType {
			continue
		}

		// Filtro per stato
		if filters.Status != "" && deadline.Status != filters.Status {
			continue
		}

		// Filtro per data from
		if filters.FromDate != nil && deadline.ExpiryDate.Before(*filters.FromDate) {
			continue
		}

		// Filtro per data to
		if filters.ToDate != nil && deadline.ExpiryDate.After(*filters.ToDate) {
			continue
		}

		// Filtro per ricerca testuale (cerca nel nome dell'entità)
		if filters.Search != "" {
			searchLower := strings.ToLower(filters.Search)
			entityNameLower := strings.ToLower(deadline.EntityName)
			if !strings.Contains(entityNameLower, searchLower) {
				continue
			}
		}

		filtered = append(filtered, deadline)
	}

	return filtered
}
