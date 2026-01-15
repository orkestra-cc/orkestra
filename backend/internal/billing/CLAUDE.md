# Module: Billing - Fatturazione Elettronica Italiana

_Path: `/backend/internal/billing`_
_Parent: [../../CLAUDE.md](../../CLAUDE.md)_

<!-- Navigation -->

[← Backend](../../CLAUDE.md) | [☰ Module Map](../../../../CLAUDE.md#module-map)

<!-- /Navigation -->

## Module Purpose

The billing module handles **Italian electronic invoicing** (Fatturazione Elettronica) through integration with the OpenAPI SDI (Sistema di Interscambio) service.

- **Primary Role**: Create, send, and receive electronic invoices compliant with FatturaPA format
- **External Integration**: OpenAPI SDI for invoice transmission and notification retrieval
- **Conditional Activation**: Module activates only when `OPENAPI_BEARER_TOKEN` is configured

**IMPORTANT**: This module is disabled by default. Configure the OpenAPI SDI credentials to enable billing functionality.

## API Compliance - MANDATORY

**All billing endpoints MUST align with the OpenAPI SDI specification:**

- **Spec URL**: `https://console.openapi.com/oas/it/sdi.openapi.json`
- **Provider**: OpenAPI.it SDI (Sistema di Interscambio)
- **Documentation**: [OpenAPI SDI Documentation](https://www.openapi.it/documentazione-sdi/)

### Compliance Requirements

When modifying or adding billing endpoints:

1. **Verify alignment** with the OpenAPI SDI spec before implementation
2. **Match request/response schemas** to the official specification
3. **Use correct endpoint variants** for signature and legal storage options
4. **Follow FatturaPA XML schema** v1.2.2 for invoice structure

### Implementation Notes

1. **Notification Strategy**: This module uses **polling** instead of webhooks
   - Simpler deployment (no public callback URL required)
   - Configurable interval via `OPENAPI_POLLING_INTERVAL`
   - Both approaches are valid per the spec

2. **Invoice Submission Variants** (per OpenAPI SDI spec):
   - `/invoices` - Basic submission
   - `/invoices_signature` - With digital signature
   - `/invoices_legal_storage` - With legal storage
   - `/invoices_signature_legal_storage` - Both signature and storage

3. **Required Registration**: Users must register recipient code `JKKZDGR` with Agenzia delle Entrate before receiving supplier invoices.

## Module Structure

```
billing/
├── config/
│   └── openapi_config.go       # OpenAPI SDI configuration
├── models/
│   ├── enums.go                # Status, document types, notification types
│   ├── party.go                # Customer/Supplier models
│   ├── invoice.go              # Invoice model with line items
│   ├── notification.go         # SDI notification models
│   ├── dto.go                  # Request/Response DTOs
│   └── xml_types.go            # FatturaPA XML structure types
├── repository/
│   ├── invoice_repository.go   # Invoice CRUD operations
│   ├── customer_repository.go  # Customer management
│   ├── supplier_repository.go  # Supplier management
│   └── notification_repository.go # SDI notifications
├── services/
│   ├── invoice_service.go      # Invoice business logic
│   ├── customer_service.go     # Customer service
│   ├── supplier_service.go     # Supplier service
│   ├── notification_service.go # Notification processing
│   ├── openapi_client.go       # HTTP client for OpenAPI SDI
│   └── xml_builder.go          # FatturaPA XML generation
├── handlers/
│   ├── invoice_handler.go      # Invoice HTTP handlers
│   ├── customer_handler.go     # Customer HTTP handlers
│   ├── supplier_handler.go     # Supplier HTTP handlers
│   └── notification_handler.go # Notification HTTP handlers
├── jobs/
│   └── polling_job.go          # Background SDI notification polling
├── routes.go                   # Huma v2 route registration
└── CLAUDE.md                   # This file
```

## API Endpoints

### Invoices (Fatture Attive)

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/billing/invoices` | Create draft invoice |
| GET | `/api/v1/billing/invoices` | List invoices with filtering |
| GET | `/api/v1/billing/invoices/{id}` | Get invoice by UUID |
| PATCH | `/api/v1/billing/invoices/{id}` | Update draft invoice |
| DELETE | `/api/v1/billing/invoices/{id}` | Delete draft invoice |
| POST | `/api/v1/billing/invoices/{id}/send` | Send invoice to SDI |
| GET | `/api/v1/billing/invoices/{id}/xml` | Get FatturaPA XML |
| GET | `/api/v1/billing/invoices/{id}/html` | Get HTML view of invoice |
| POST | `/api/v1/billing/invoices/import` | Import supplier invoice (base64 XML) |

### Received Invoices (Fatture Passive)

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/billing/received-invoices` | List received invoices |
| GET | `/api/v1/billing/received-invoices/{id}` | Get received invoice |
| POST | `/api/v1/billing/received-invoices/{id}/accept` | Accept invoice |
| POST | `/api/v1/billing/received-invoices/{id}/reject` | Reject invoice |

### Customers & Suppliers

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST/GET/PATCH/DELETE | `/api/v1/billing/customers[/{id}]` | Customer CRUD |
| POST/GET/PATCH/DELETE | `/api/v1/billing/suppliers[/{id}]` | Supplier CRUD |

### Notifications & Statistics

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/billing/notifications` | List SDI notifications |
| GET | `/api/v1/billing/notifications/{id}` | Get notification |
| POST | `/api/v1/billing/notifications/{id}/process` | Mark as processed |
| GET | `/api/v1/billing/notifications/summary` | Notification summary |
| GET | `/api/v1/billing/stats` | Billing statistics |

### Preserved Documents (Legal Storage)

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/billing/preserved-documents/{id}` | Get preservation status |

## OpenAPI SDI Integration

### Invoice Lifecycle

```
Draft → Sent → [SDI Processing] → Delivered/Rejected
                    ↓
              Notifications (RC, NS, MC, NE, DT, AT)
```

### SDI Notification Types

| Code | Name | Description |
|------|------|-------------|
| RC | Ricevuta di Consegna | Invoice delivered to recipient |
| NS | Notifica di Scarto | Invoice rejected by SDI (format errors) |
| MC | Mancata Consegna | Delivery failed (recipient unreachable) |
| NE | Notifica Esito | PA acceptance/rejection (public sector only) |
| DT | Decorrenza Termini | Silent acceptance (15 days elapsed) |
| AT | Attestazione | Transmission attestation |

### Polling Job

The `polling_job.go` runs a background goroutine that periodically fetches new notifications from OpenAPI SDI:

- Polls at configurable intervals (`OPENAPI_POLLING_INTERVAL`)
- Updates invoice status based on notification type
- Stores notifications for audit trail
- Handles errors with exponential backoff

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `OPENAPI_BASE_URL` | OpenAPI SDI API URL | `https://test.sdi.openapi.it` |
| `OPENAPI_BEARER_TOKEN` | Authentication token | _(required to enable)_ |
| `OPENAPI_FISCAL_ID` | Company fiscal code (P.IVA) | _(required)_ |
| `OPENAPI_RECIPIENT_CODE` | Default recipient code | `JKKZDGR` |
| `OPENAPI_APPLY_SIGNATURE` | Enable digital signature | `true` |
| `OPENAPI_APPLY_STORAGE` | Enable legal storage | `true` |
| `OPENAPI_TIMEOUT` | HTTP request timeout | `30s` |
| `OPENAPI_RETRY_ATTEMPTS` | Retry count on failure | `3` |
| `OPENAPI_POLLING_INTERVAL` | Notification poll interval | `5m` |
| `OPENAPI_SANDBOX_MODE` | Use sandbox environment | `true` |

### Production vs Sandbox

| Environment | Base URL |
|-------------|----------|
| Sandbox | `https://test.sdi.openapi.it` |
| Production | `https://sdi.openapi.it` |

## Development Guidelines

### Module Activation

The module is conditionally activated in `main.go`:

```go
if cfg.OpenAPI.BearerToken != "" {
    // Initialize billing module
    billing.RegisterRoutes(api, ...)
    go pollingJob.Start(ctx)
} else {
    logger.Warn("Billing module disabled: OPENAPI_BEARER_TOKEN not configured")
}
```

### Testing with Sandbox

1. Get sandbox credentials from OpenAPI SDI
2. Configure environment variables in `.env.development`
3. Set `OPENAPI_SANDBOX_MODE=true`
4. Test invoice creation and sending
5. Verify notification polling

### FatturaPA XML Format

The `xml_builder.go` generates XML compliant with:
- FatturaPA v1.2.3 schema (Specifiche Tecniche v1.9 - effective April 2025)
- Agenzia delle Entrate specifications
- Required fields for B2B and B2G invoices

**See [FATTURAPA_SPEC.md](FATTURAPA_SPEC.md)** for complete bilingual field reference including:
- Complete XML element hierarchy with cardinality
- All enumerations (TD01-TD29, RF01-RF20, N1-N7.x, MP01-MP23)
- SDI validation rules and error codes
- v1.9 updates (TD29, RF20)

### Error Handling

SDI errors are mapped to appropriate HTTP responses:
- Validation errors → 400 Bad Request
- Authentication errors → 401 Unauthorized
- SDI rejection → 422 Unprocessable Entity
- Network errors → 503 Service Unavailable

## MongoDB Collections

| Collection | Description |
|------------|-------------|
| `billing_invoices` | Invoice documents |
| `billing_customers` | Customer registry |
| `billing_suppliers` | Supplier registry |
| `billing_notifications` | SDI notifications |
| `billing_polling_state` | Polling job state |

### Indexes

Ensure these indexes exist for performance:
- `billing_invoices`: `number`, `customer_id`, `status`, `created_at`
- `billing_customers`: `fiscal_code`, `vat_number`
- `billing_notifications`: `invoice_uuid`, `notification_type`, `processed`

## Security Considerations

- Store `OPENAPI_BEARER_TOKEN` securely (never in code)
- Validate all fiscal codes and VAT numbers
- Sanitize XML content to prevent injection
- Audit log all invoice operations
- Implement rate limiting on send operations

---

### Related Guides

- [FatturaPA XML Specification](FATTURAPA_SPEC.md) - Complete bilingual XML field reference (IT/EN)
- [Backend Module](../../CLAUDE.md) - Backend architecture and development workflow
- [Project Overview](../../../../CLAUDE.md) - System architecture
- [OpenAPI SDI Documentation](https://www.openapi.it/documentazione-sdi/) - External API docs
