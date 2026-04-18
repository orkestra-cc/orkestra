package services

import (
	"context"
	"errors"

	"github.com/orkestra/backend/internal/addons/subscriptions/repository"
)

// ClientOwnershipService answers "which org owns this client?" for other
// modules (payments) that hold a bare ClientUUID and need to gate by tenant.
// Implements iface.ClientOwnershipProvider via structural typing.
type ClientOwnershipService struct {
	clients repository.ClientRepository
}

func NewClientOwnershipService(clients repository.ClientRepository) *ClientOwnershipService {
	return &ClientOwnershipService{clients: clients}
}

// ErrClientNotFound is returned when the UUID does not match any client.
// Callers should translate this to a 404 without leaking internal state.
var ErrClientNotFound = errors.New("subscriptions: client not found")

// GetClientOrgUUID returns the owning org UUID (possibly empty) or
// ErrClientNotFound if the client does not exist.
func (s *ClientOwnershipService) GetClientOrgUUID(ctx context.Context, clientUUID string) (string, error) {
	if clientUUID == "" {
		return "", ErrClientNotFound
	}
	c, err := s.clients.GetByUUID(ctx, clientUUID)
	if err != nil {
		if errors.Is(err, repository.ErrClientNotFound) {
			return "", ErrClientNotFound
		}
		return "", err
	}
	return c.OrgUUID, nil
}
