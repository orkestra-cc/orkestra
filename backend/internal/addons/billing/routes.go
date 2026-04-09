package billing

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/orkestra/backend/internal/addons/billing/handlers"
)

// RegisterRoutes registers all billing module routes
func RegisterRoutes(
	api huma.API,
	invoiceHandler *handlers.InvoiceHandler,
	customerHandler *handlers.CustomerHandler,
	supplierHandler *handlers.SupplierHandler,
	companyHandler *handlers.CompanyHandler,
	notificationHandler *handlers.NotificationHandler,
	businessRegistryHandler *handlers.BusinessRegistryHandler,
	syncHandler *handlers.SyncHandler,
) {
	// ========================================
	// Invoice Routes (Issued - Fatture Attive)
	// ========================================
	huma.Register(api, huma.Operation{
		OperationID: "create-invoice",
		Method:      http.MethodPost,
		Path:        "/v1/billing/invoices",
		Summary:     "Create invoice",
		Description: "Creates a new draft invoice for emission",
		Tags:        []string{"Billing - Invoices"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, invoiceHandler.CreateInvoice)

	huma.Register(api, huma.Operation{
		OperationID: "list-invoices",
		Method:      http.MethodGet,
		Path:        "/v1/billing/invoices",
		Summary:     "List invoices",
		Description: "Lists invoices with optional filtering and pagination",
		Tags:        []string{"Billing - Invoices"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, invoiceHandler.ListInvoices)

	huma.Register(api, huma.Operation{
		OperationID: "get-invoice",
		Method:      http.MethodGet,
		Path:        "/v1/billing/invoices/{id}",
		Summary:     "Get invoice",
		Description: "Retrieves an invoice by its UUID",
		Tags:        []string{"Billing - Invoices"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, invoiceHandler.GetInvoice)

	huma.Register(api, huma.Operation{
		OperationID: "update-invoice",
		Method:      http.MethodPatch,
		Path:        "/v1/billing/invoices/{id}",
		Summary:     "Update invoice",
		Description: "Updates a draft invoice. Only draft invoices can be modified.",
		Tags:        []string{"Billing - Invoices"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, invoiceHandler.UpdateInvoice)

	huma.Register(api, huma.Operation{
		OperationID: "delete-invoice",
		Method:      http.MethodDelete,
		Path:        "/v1/billing/invoices/{id}",
		Summary:     "Delete invoice",
		Description: "Deletes a draft invoice. Only draft invoices can be deleted.",
		Tags:        []string{"Billing - Invoices"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, invoiceHandler.DeleteInvoice)

	huma.Register(api, huma.Operation{
		OperationID: "send-invoice",
		Method:      http.MethodPost,
		Path:        "/v1/billing/invoices/{id}/send",
		Summary:     "Send invoice to SDI",
		Description: "Sends the invoice to the SDI (Sistema di Interscambio) for delivery",
		Tags:        []string{"Billing - Invoices"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, invoiceHandler.SendInvoice)

	huma.Register(api, huma.Operation{
		OperationID: "duplicate-invoice",
		Method:      http.MethodPost,
		Path:        "/v1/billing/invoices/{id}/duplicate",
		Summary:     "Duplicate invoice",
		Description: "Creates a copy of an existing invoice as a draft without invoice number. The duplicated invoice will have status 'draft' with all fields copied except SDI-related data.",
		Tags:        []string{"Billing - Invoices"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, invoiceHandler.DuplicateInvoice)

	huma.Register(api, huma.Operation{
		OperationID: "get-invoice-xml",
		Method:      http.MethodGet,
		Path:        "/v1/billing/invoices/{id}/xml",
		Summary:     "Get invoice XML",
		Description: "Returns the FatturaPA XML content of the invoice",
		Tags:        []string{"Billing - Invoices"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, invoiceHandler.GetInvoiceXML)

	huma.Register(api, huma.Operation{
		OperationID:   "download-invoice-pdf",
		Method:        http.MethodGet,
		Path:          "/v1/billing/invoices/{id}/download",
		Summary:       "Download invoice PDF",
		Description:   "Downloads the invoice as a PDF file. For draft invoices, generates PDF locally. For sent invoices, retrieves from SDI.",
		Tags:          []string{"Billing - Invoices"},
		Security:      []map[string][]string{{"bearerAuth": {}}},
		DefaultStatus: http.StatusOK,
		Responses: map[string]*huma.Response{
			"200": {
				Description: "PDF document",
				Content: map[string]*huma.MediaType{
					"application/pdf": {
						Schema: &huma.Schema{
							Type:   huma.TypeString,
							Format: "binary",
						},
					},
				},
			},
		},
	}, invoiceHandler.GetInvoicePDF)

	huma.Register(api, huma.Operation{
		OperationID: "get-invoice-html",
		Method:      http.MethodGet,
		Path:        "/v1/billing/invoices/{id}/html",
		Summary:     "Get invoice HTML view",
		Description: "Returns the HTML representation of the invoice (only available for sent invoices). Per OpenAPI SDI spec.",
		Tags:        []string{"Billing - Invoices"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, invoiceHandler.GetInvoiceHTML)

	huma.Register(api, huma.Operation{
		OperationID: "import-invoice",
		Method:      http.MethodPost,
		Path:        "/v1/billing/invoices/import",
		Summary:     "Import supplier invoice",
		Description: "Imports a supplier invoice via base64-encoded FatturaPA XML. Per OpenAPI SDI spec.",
		Tags:        []string{"Billing - Invoices"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, invoiceHandler.ImportInvoice)

	huma.Register(api, huma.Operation{
		OperationID: "import-xml-invoice",
		Method:      http.MethodPost,
		Path:        "/v1/billing/invoices/import-xml",
		Summary:     "Import invoice via native XML parsing",
		Description: "Imports received invoices (fatture passive) via native FatturaPA XML parsing. Supports raw XML or base64-encoded XML. Creates supplier if not exists. Handles batch invoices (multiple bodies in single XML).",
		Tags:        []string{"Billing - Invoices"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, invoiceHandler.ImportXMLInvoice)

	// ========================================
	// Received Invoice Routes (Fatture Passive)
	// ========================================
	huma.Register(api, huma.Operation{
		OperationID: "list-received-invoices",
		Method:      http.MethodGet,
		Path:        "/v1/billing/received-invoices",
		Summary:     "List received invoices",
		Description: "Lists received invoices (fatture passive) with optional filtering",
		Tags:        []string{"Billing - Received Invoices"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, invoiceHandler.ListInvoices) // Reuses ListInvoices with direction=received

	huma.Register(api, huma.Operation{
		OperationID: "get-received-invoice",
		Method:      http.MethodGet,
		Path:        "/v1/billing/received-invoices/{id}",
		Summary:     "Get received invoice",
		Description: "Retrieves a received invoice by its UUID",
		Tags:        []string{"Billing - Received Invoices"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, invoiceHandler.GetInvoice)

	huma.Register(api, huma.Operation{
		OperationID: "accept-received-invoice",
		Method:      http.MethodPost,
		Path:        "/v1/billing/received-invoices/{id}/accept",
		Summary:     "Accept received invoice",
		Description: "Marks a received invoice as accepted",
		Tags:        []string{"Billing - Received Invoices"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, invoiceHandler.AcceptReceivedInvoice)

	huma.Register(api, huma.Operation{
		OperationID: "reject-received-invoice",
		Method:      http.MethodPost,
		Path:        "/v1/billing/received-invoices/{id}/reject",
		Summary:     "Reject received invoice",
		Description: "Marks a received invoice as rejected with a reason",
		Tags:        []string{"Billing - Received Invoices"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, invoiceHandler.RejectReceivedInvoice)

	// ========================================
	// Customer Routes
	// ========================================
	huma.Register(api, huma.Operation{
		OperationID: "create-customer",
		Method:      http.MethodPost,
		Path:        "/v1/billing/customers",
		Summary:     "Create customer",
		Description: "Creates a new billing customer for invoice emission",
		Tags:        []string{"Billing - Customers"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, customerHandler.CreateCustomer)

	huma.Register(api, huma.Operation{
		OperationID: "list-customers",
		Method:      http.MethodGet,
		Path:        "/v1/billing/customers",
		Summary:     "List customers",
		Description: "Lists billing customers with optional search and pagination",
		Tags:        []string{"Billing - Customers"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, customerHandler.ListCustomers)

	huma.Register(api, huma.Operation{
		OperationID: "get-customer",
		Method:      http.MethodGet,
		Path:        "/v1/billing/customers/{id}",
		Summary:     "Get customer",
		Description: "Retrieves a billing customer by its UUID",
		Tags:        []string{"Billing - Customers"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, customerHandler.GetCustomer)

	huma.Register(api, huma.Operation{
		OperationID: "update-customer",
		Method:      http.MethodPatch,
		Path:        "/v1/billing/customers/{id}",
		Summary:     "Update customer",
		Description: "Updates a billing customer's information",
		Tags:        []string{"Billing - Customers"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, customerHandler.UpdateCustomer)

	huma.Register(api, huma.Operation{
		OperationID: "delete-customer",
		Method:      http.MethodDelete,
		Path:        "/v1/billing/customers/{id}",
		Summary:     "Delete customer",
		Description: "Soft deletes (deactivates) a billing customer",
		Tags:        []string{"Billing - Customers"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, customerHandler.DeleteCustomer)

	// ========================================
	// Supplier Routes
	// ========================================
	huma.Register(api, huma.Operation{
		OperationID: "create-supplier",
		Method:      http.MethodPost,
		Path:        "/v1/billing/suppliers",
		Summary:     "Create supplier",
		Description: "Creates a new billing supplier for received invoices",
		Tags:        []string{"Billing - Suppliers"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, supplierHandler.CreateSupplier)

	huma.Register(api, huma.Operation{
		OperationID: "list-suppliers",
		Method:      http.MethodGet,
		Path:        "/v1/billing/suppliers",
		Summary:     "List suppliers",
		Description: "Lists billing suppliers with optional search and pagination",
		Tags:        []string{"Billing - Suppliers"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, supplierHandler.ListSuppliers)

	huma.Register(api, huma.Operation{
		OperationID: "get-supplier",
		Method:      http.MethodGet,
		Path:        "/v1/billing/suppliers/{id}",
		Summary:     "Get supplier",
		Description: "Retrieves a billing supplier by its UUID",
		Tags:        []string{"Billing - Suppliers"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, supplierHandler.GetSupplier)

	huma.Register(api, huma.Operation{
		OperationID: "update-supplier",
		Method:      http.MethodPatch,
		Path:        "/v1/billing/suppliers/{id}",
		Summary:     "Update supplier",
		Description: "Updates a billing supplier's information",
		Tags:        []string{"Billing - Suppliers"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, supplierHandler.UpdateSupplier)

	huma.Register(api, huma.Operation{
		OperationID: "delete-supplier",
		Method:      http.MethodDelete,
		Path:        "/v1/billing/suppliers/{id}",
		Summary:     "Delete supplier",
		Description: "Soft deletes (deactivates) a billing supplier",
		Tags:        []string{"Billing - Suppliers"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, supplierHandler.DeleteSupplier)

	// ========================================
	// Company Routes (Issuing Companies / Settings)
	// ========================================
	huma.Register(api, huma.Operation{
		OperationID: "create-company",
		Method:      http.MethodPost,
		Path:        "/v1/billing/companies",
		Summary:     "Create company",
		Description: "Creates a new issuing company for invoice emission",
		Tags:        []string{"Billing - Companies"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, companyHandler.CreateCompany)

	huma.Register(api, huma.Operation{
		OperationID: "list-companies",
		Method:      http.MethodGet,
		Path:        "/v1/billing/companies",
		Summary:     "List companies",
		Description: "Lists issuing companies with optional search and pagination",
		Tags:        []string{"Billing - Companies"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, companyHandler.ListCompanies)

	huma.Register(api, huma.Operation{
		OperationID: "get-default-company",
		Method:      http.MethodGet,
		Path:        "/v1/billing/companies/default",
		Summary:     "Get default company",
		Description: "Retrieves the default issuing company for new invoices",
		Tags:        []string{"Billing - Companies"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, companyHandler.GetDefaultCompany)

	huma.Register(api, huma.Operation{
		OperationID: "get-company",
		Method:      http.MethodGet,
		Path:        "/v1/billing/companies/{id}",
		Summary:     "Get company",
		Description: "Retrieves an issuing company by its UUID",
		Tags:        []string{"Billing - Companies"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, companyHandler.GetCompany)

	huma.Register(api, huma.Operation{
		OperationID: "update-company",
		Method:      http.MethodPatch,
		Path:        "/v1/billing/companies/{id}",
		Summary:     "Update company",
		Description: "Updates an issuing company's information",
		Tags:        []string{"Billing - Companies"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, companyHandler.UpdateCompany)

	huma.Register(api, huma.Operation{
		OperationID: "delete-company",
		Method:      http.MethodDelete,
		Path:        "/v1/billing/companies/{id}",
		Summary:     "Delete company",
		Description: "Soft deletes (deactivates) an issuing company",
		Tags:        []string{"Billing - Companies"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, companyHandler.DeleteCompany)

	huma.Register(api, huma.Operation{
		OperationID: "set-default-company",
		Method:      http.MethodPost,
		Path:        "/v1/billing/companies/{id}/default",
		Summary:     "Set default company",
		Description: "Sets a company as the default for new invoices",
		Tags:        []string{"Billing - Companies"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, companyHandler.SetDefaultCompany)

	// ========================================
	// Notification Routes
	// ========================================
	huma.Register(api, huma.Operation{
		OperationID: "list-notifications",
		Method:      http.MethodGet,
		Path:        "/v1/billing/notifications",
		Summary:     "List SDI notifications",
		Description: "Lists SDI notifications with optional filtering",
		Tags:        []string{"Billing - Notifications"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, notificationHandler.ListNotifications)

	huma.Register(api, huma.Operation{
		OperationID: "get-notification",
		Method:      http.MethodGet,
		Path:        "/v1/billing/notifications/{id}",
		Summary:     "Get notification",
		Description: "Retrieves an SDI notification by its UUID",
		Tags:        []string{"Billing - Notifications"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, notificationHandler.GetNotification)

	huma.Register(api, huma.Operation{
		OperationID: "mark-notification-processed",
		Method:      http.MethodPost,
		Path:        "/v1/billing/notifications/{id}/process",
		Summary:     "Mark notification as processed",
		Description: "Marks an SDI notification as processed",
		Tags:        []string{"Billing - Notifications"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, notificationHandler.MarkAsProcessed)

	huma.Register(api, huma.Operation{
		OperationID: "get-notification-summary",
		Method:      http.MethodGet,
		Path:        "/v1/billing/notifications/summary",
		Summary:     "Get notification summary",
		Description: "Returns a summary of SDI notifications",
		Tags:        []string{"Billing - Notifications"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, notificationHandler.GetSummary)

	// ========================================
	// Statistics Routes
	// ========================================
	huma.Register(api, huma.Operation{
		OperationID: "get-billing-stats",
		Method:      http.MethodGet,
		Path:        "/v1/billing/stats",
		Summary:     "Get billing statistics",
		Description: "Returns billing statistics for the specified period",
		Tags:        []string{"Billing - Statistics"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, invoiceHandler.GetStats)

	// ========================================
	// Preserved Documents Routes (Legal Storage)
	// ========================================
	huma.Register(api, huma.Operation{
		OperationID: "get-preserved-document",
		Method:      http.MethodGet,
		Path:        "/v1/billing/preserved-documents/{id}",
		Summary:     "Get preserved document status",
		Description: "Returns the legal storage/preservation status of a document. Per OpenAPI SDI spec.",
		Tags:        []string{"Billing - Preserved Documents"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, invoiceHandler.GetPreservedDocument)

	// ========================================
	// Business Registry Configuration Routes
	// ========================================
	huma.Register(api, huma.Operation{
		OperationID: "get-business-registry-config",
		Method:      http.MethodGet,
		Path:        "/v1/billing/business-registry/{fiscalId}",
		Summary:     "Get business registry configuration",
		Description: "Retrieves the OpenAPI SDI business registry configuration for a fiscal ID",
		Tags:        []string{"Billing - Business Registry"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, businessRegistryHandler.GetConfig)

	huma.Register(api, huma.Operation{
		OperationID: "configure-business-registry",
		Method:      http.MethodPost,
		Path:        "/v1/billing/business-registry",
		Summary:     "Configure business registry",
		Description: "Creates or updates the OpenAPI SDI business registry configuration. Required before sending invoices.",
		Tags:        []string{"Billing - Business Registry"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, businessRegistryHandler.Configure)

	// ========================================
	// Webhook Routes (registered separately on public API via RegisterWebhookRoutes)
	// ========================================

	// ========================================
	// Manual Sync Routes
	// ========================================
	if syncHandler != nil {
		huma.Register(api, huma.Operation{
			OperationID: "sync-all",
			Method:      http.MethodPost,
			Path:        "/v1/billing/sync",
			Summary:     "Sync all SDI data",
			Description: "Manually triggers a full sync with OpenAPI SDI (invoices + notifications). Use this instead of automatic polling to control API usage.",
			Tags:        []string{"Billing - Sync"},
			Security:    []map[string][]string{{"bearerAuth": {}}},
		}, syncHandler.SyncAll)

		huma.Register(api, huma.Operation{
			OperationID: "sync-invoices",
			Method:      http.MethodPost,
			Path:        "/v1/billing/sync/invoices",
			Summary:     "Sync invoices only",
			Description: "Manually triggers invoice sync with OpenAPI SDI. Imports issued and received invoices from the last 30 days.",
			Tags:        []string{"Billing - Sync"},
			Security:    []map[string][]string{{"bearerAuth": {}}},
		}, syncHandler.SyncInvoices)
	}
}

// RegisterWebhookRoutes registers public webhook routes (no JWT authentication)
func RegisterWebhookRoutes(api huma.API, webhookHandler *handlers.WebhookHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "sdi-webhook",
		Method:      http.MethodPost,
		Path:        "/v1/billing/webhooks/sdi",
		Summary:     "SDI Webhook Callback",
		Description: "Receives real-time callbacks from OpenAPI SDI. Public endpoint authenticated via webhook secret.",
		Tags:        []string{"Billing - Webhooks"},
		Security:    []map[string][]string{},
	}, webhookHandler.HandleCallback)
}
