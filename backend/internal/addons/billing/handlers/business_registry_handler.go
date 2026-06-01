package handlers

import (
	"context"
	"errors"
	"fmt"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/addons/billing/services"
)

// BusinessRegistryHandler handles business registry configuration requests
type BusinessRegistryHandler struct {
	openAPIClient services.OpenAPIClient
}

// NewBusinessRegistryHandler creates a new BusinessRegistryHandler
func NewBusinessRegistryHandler(openAPIClient services.OpenAPIClient) *BusinessRegistryHandler {
	return &BusinessRegistryHandler{
		openAPIClient: openAPIClient,
	}
}

// ========================================
// Request/Response Types
// ========================================

// GetBusinessRegistryConfigRequest represents the request to get business registry config
type GetBusinessRegistryConfigRequest struct {
	FiscalID string `path:"fiscalId" doc:"Fiscal ID (P.IVA)"`
}

// GetBusinessRegistryConfigResponse represents the response with business registry config
type GetBusinessRegistryConfigResponse struct {
	Body BusinessRegistryConfigOutput `json:"config" doc:"Business registry configuration"`
}

// ConfigureBusinessRegistryRequest represents the request to configure business registry
type ConfigureBusinessRegistryRequest struct {
	Body ConfigureBusinessRegistryInput `json:"config" doc:"Business registry configuration to set"`
}

// ConfigureBusinessRegistryResponse represents the response after configuring business registry
type ConfigureBusinessRegistryResponse struct {
	Body struct {
		Success bool   `json:"success" doc:"Whether the operation was successful"`
		Message string `json:"message" doc:"Success or error message"`
	}
}

// ConfigureBusinessRegistryInput represents the input for configuring business registry
type ConfigureBusinessRegistryInput struct {
	FiscalID          string `json:"fiscalId" validate:"required" doc:"Fiscal ID (P.IVA)"`
	Email             string `json:"email" validate:"required,email" doc:"Email for SDI notifications"`
	ApplySignature    bool   `json:"applySignature" doc:"Enable digital signature on invoices"`
	ApplyLegalStorage bool   `json:"applyLegalStorage" doc:"Enable legal storage (conservazione sostitutiva)"`
}

// BusinessRegistryConfigOutput represents the output for business registry config
type BusinessRegistryConfigOutput struct {
	FiscalID          string `json:"fiscalId" doc:"Fiscal ID (P.IVA)"`
	Email             string `json:"email" doc:"Email for SDI notifications"`
	ApplySignature    bool   `json:"applySignature" doc:"Digital signature enabled"`
	ApplyLegalStorage bool   `json:"applyLegalStorage" doc:"Legal storage enabled"`
	Active            bool   `json:"active" doc:"Whether the configuration is active"`
}

// ========================================
// Handler Methods
// ========================================

// GetConfig retrieves the business registry configuration for a fiscal ID
func (h *BusinessRegistryHandler) GetConfig(ctx context.Context, req *GetBusinessRegistryConfigRequest) (*GetBusinessRegistryConfigResponse, error) {
	config, err := h.openAPIClient.GetBusinessRegistryConfig(ctx, req.FiscalID)
	if err != nil {
		if err == services.ErrOpenAPINotFound {
			return nil, huma.Error404NotFound("Business registry configuration not found for fiscal ID: "+req.FiscalID, err)
		}
		return nil, huma.Error500InternalServerError("Failed to get business registry configuration", err)
	}

	return &GetBusinessRegistryConfigResponse{
		Body: BusinessRegistryConfigOutput{
			FiscalID:          config.FiscalID,
			Email:             config.Email,
			ApplySignature:    config.ApplySignature,
			ApplyLegalStorage: config.ApplyLegalStorage,
			Active:            config.Active,
		},
	}, nil
}

// Configure creates or updates a business registry configuration
func (h *BusinessRegistryHandler) Configure(ctx context.Context, req *ConfigureBusinessRegistryRequest) (*ConfigureBusinessRegistryResponse, error) {
	config := services.BusinessRegistryConfig{
		FiscalID:          req.Body.FiscalID,
		Email:             req.Body.Email,
		ApplySignature:    req.Body.ApplySignature,
		ApplyLegalStorage: req.Body.ApplyLegalStorage,
	}

	err := h.openAPIClient.ConfigureBusinessRegistry(ctx, config)
	if err != nil {
		// Check for specific OpenAPI SDI errors
		if errors.Is(err, services.ErrOpenAPIEmailAlreadyExists) {
			return nil, huma.Error409Conflict(
				"Questa email è già registrata per un'altra Partita IVA su OpenAPI SDI. Utilizzare un'email diversa.",
				err,
			)
		}

		var apiErr *services.OpenAPIError
		if errors.As(err, &apiErr) {
			if apiErr.Code == 389 {
				return nil, huma.Error400BadRequest(
					"Configurazione mancante per questa Partita IVA su OpenAPI SDI. Verificare le credenziali.",
					err,
				)
			}
			if apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 {
				return nil, huma.Error409Conflict(
					fmt.Sprintf("Errore OpenAPI SDI (codice %d): %s", apiErr.Code, apiErr.Message),
					err,
				)
			}
		}

		return nil, huma.Error500InternalServerError("Errore durante la configurazione del Business Registry: "+err.Error(), err)
	}

	resp := &ConfigureBusinessRegistryResponse{}
	resp.Body.Success = true
	resp.Body.Message = "Business registry configurato con successo per Partita IVA: " + req.Body.FiscalID

	return resp, nil
}
