# Module: Documents - PDF Generation Service

_Path: `/backend/internal/addons/documents`_
_Parent: [../../../CLAUDE.md](../../../CLAUDE.md)_

<!-- Navigation -->

[← Backend](../../../CLAUDE.md) | [☰ Module Map](../../../../../CLAUDE.md#module-map)

<!-- /Navigation -->

## Module Purpose

The documents module provides **template-based PDF generation** for invoices, offers, and other company documents using HTML/CSS templates with Go's `html/template` engine and **Gotenberg** (Chromium-based) as the PDF rendering sidecar.

- **Primary Role**: Generate professional PDF documents from customizable HTML templates
- **PDF Engine**: Gotenberg (Docker sidecar) - production-ready, excellent CSS support
- **Template Storage**: MongoDB with embedded default templates
- **Generation Strategy**: On-demand generation (no caching - always fresh)

## Dependencies

### External Services

- **Gotenberg**: Chromium-based PDF generation service (runs as Docker sidecar on port 3030)

### Internal Dependencies

- **MongoDB**: Template and generated document storage
- **Billing Module**: Integrates for invoice PDF generation

## Module Structure

```
documents/
├── config/
│   └── config.go              # Gotenberg configuration
├── models/
│   ├── template.go            # Template model
│   ├── document.go            # Generated document model
│   ├── dto.go                 # Request/Response DTOs
│   └── enums.go               # TemplateType, PageSize, etc.
├── repository/
│   ├── template_repository.go # Template CRUD
│   └── document_repository.go # Generated docs storage
├── services/
│   ├── pdf_service.go         # PDF generation orchestration
│   ├── gotenberg_client.go    # HTTP client for Gotenberg
│   ├── template_service.go    # Template management
│   └── template_engine.go     # Go template rendering
├── handlers/
│   ├── template_handler.go    # Template CRUD endpoints
│   └── document_handler.go    # PDF generation endpoints
├── routes.go                  # Huma v2 route registration
└── CLAUDE.md                  # This file
```

## API Endpoints

### Template Management

| Method | Path | Description |
|--------|------|-------------|
| POST | `/v1/documents/templates` | Create template |
| GET | `/v1/documents/templates` | List templates (with filters) |
| GET | `/v1/documents/templates/{id}` | Get template by UUID |
| PATCH | `/v1/documents/templates/{id}` | Update template |
| DELETE | `/v1/documents/templates/{id}` | Delete template (soft delete) |
| POST | `/v1/documents/templates/{id}/default` | Set as default for type |
| POST | `/v1/documents/templates/{id}/duplicate` | Duplicate template |
| GET | `/v1/documents/templates/variables/{type}` | Get available variables |

### PDF Generation

| Method | Path | Description |
|--------|------|-------------|
| POST | `/v1/documents/generate` | Generate PDF from template + data |
| POST | `/v1/documents/preview` | Preview HTML (no PDF) |
| POST | `/v1/documents/preview/content` | Preview raw HTML/CSS |
| GET | `/v1/documents/{id}` | Get document metadata |
| GET | `/v1/documents/{id}/download` | Download PDF binary |
| GET | `/v1/documents/status` | Check Gotenberg availability |

## Template Types

- **invoice**: Italian invoices (Fattura) with FatturaPA fields
- **offer**: Quotes/proposals (Preventivo)
- **receipt**: Payment receipts
- **custom**: User-defined document types

## Template Functions

The template engine provides these built-in functions:

### Date Formatting
```go
{{formatDate .date "02/01/2006"}}     // Custom format
{{formatDateIT .date}}                 // Italian format (dd/mm/yyyy)
{{formatDateISO .date}}                // ISO format (yyyy-mm-dd)
{{formatDateTime .date}}               // With time (dd/mm/yyyy HH:MM)
{{now}}                                // Current time
```

### Money Formatting
```go
{{formatMoney .amount 2 "EUR"}}        // Amount with decimals and symbol
{{formatMoneyEUR .amount}}             // Italian locale (1.234,56 EUR)
{{formatMoneySymbol .amount "€"}}      // Symbol prefix
```

### Number Formatting
```go
{{formatNumber .value 2}}              // Fixed decimals
{{formatNumberIT .value 2}}            // Italian locale (1.234,56)
{{formatPercent .value 1}}             // As percentage (22.0%)
```

### String Manipulation
```go
{{upper .text}}                        // UPPERCASE
{{lower .text}}                        // lowercase
{{trim .text}}                         // Remove whitespace
{{title .text}}                        // Title Case
{{truncate .text 50}}                  // Truncate with ellipsis
{{replace .text "old" "new"}}          // Replace all occurrences
```

### Table Helpers
```go
{{range $i, $line := .lines}}
  <tr>
    <td>{{lineNumber $i}}</td>         // 1, 2, 3... (0-indexed to 1-indexed)
  </tr>
{{end}}
```

### Math
```go
{{add 1 2}}                            // 3
{{sub 5 2}}                            // 3
{{mul 3.0 4.0}}                        // 12.0
{{div 10.0 3.0}}                       // 3.333...
```

### Conditional
```go
{{default "N/A" .value}}               // Default if nil/empty
{{coalesce .a .b .c}}                  // First non-nil value
{{ifelse .condition "yes" "no"}}       // Ternary
```

### Comments
```html
<!-- HTML comment (visible in HTML source, not in PDF) -->

{{/* Go template comment (completely stripped from output) */}}
```

**Important:** Go template comments `{{/* */}}` cannot contain template directives like `{{.field}}` or `{{formatMoney .amount}}`. The parser will fail with "unexpected character in command" errors.

```html
{{/* This is OK */}}

{{/*
  Multi-line comment without template directives is OK
  Just plain text here
*/}}

{{/* This will FAIL: {{.someField}} */}}
```

If you need to comment out code that contains template directives, use HTML comments instead:
```html
<!--
  <div>{{formatMoneyEUR .amount}}</div>
-->
```

## Example: Invoice Template

```html
<!DOCTYPE html>
<html lang="it">
<head>
  <meta charset="UTF-8">
  <title>Fattura {{.number}}</title>
</head>
<body>
  <div class="header">
    <h1>FATTURA N. {{.number}}</h1>
    <p>Data: {{formatDateIT .date}}</p>
  </div>

  <div class="seller">
    <strong>{{.seller.name}}</strong><br>
    {{.seller.address}}<br>
    P.IVA: {{.seller.vatNumber}}
  </div>

  <div class="buyer">
    <strong>Spett.le {{.buyer.name}}</strong><br>
    {{.buyer.address}}<br>
    {{if .buyer.vatNumber}}P.IVA: {{.buyer.vatNumber}}{{end}}
  </div>

  <table>
    <thead>
      <tr>
        <th>#</th>
        <th>Descrizione</th>
        <th>Q.tà</th>
        <th>Prezzo</th>
        <th>IVA</th>
        <th>Totale</th>
      </tr>
    </thead>
    <tbody>
      {{range $i, $line := .lines}}
      <tr>
        <td>{{lineNumber $i}}</td>
        <td>{{$line.Description}}</td>
        <td>{{formatNumber $line.Quantity 2}}</td>
        <td>{{formatMoneyEUR $line.UnitPrice}}</td>
        <td>{{formatNumber $line.VATRate 0}}%</td>
        <td>{{formatMoneyEUR $line.TotalPrice}}</td>
      </tr>
      {{end}}
    </tbody>
  </table>

  <div class="totals">
    <p>Imponibile: {{formatMoneyEUR .totalTaxable}}</p>
    <p>IVA: {{formatMoneyEUR .totalVAT}}</p>
    <p><strong>TOTALE: {{formatMoneyEUR .totalAmount}}</strong></p>
  </div>
</body>
</html>
```

## Configuration

### Environment Variables

```bash
# Gotenberg service URL
GOTENBERG_URL=http://gotenberg:3000

# Request timeout
GOTENBERG_TIMEOUT=60s

# Retry attempts for failed requests
GOTENBERG_RETRY_ATTEMPTS=3
```

### Docker Compose

The Gotenberg service is defined in `docker-compose.infra.yml`:

```yaml
gotenberg:
  image: gotenberg/gotenberg:8
  container_name: orkestra-gotenberg
  restart: unless-stopped
  ports:
    - "${GOTENBERG_PORT:-3030}:3000"
  command:
    - "gotenberg"
    - "--api-timeout=120s"
    - "--chromium-max-queue-size=20"
  networks:
    - orkestra-network
  healthcheck:
    test: ["CMD", "curl", "-f", "http://localhost:3000/health"]
    interval: 10s
    timeout: 5s
    retries: 5
```

## Service Architecture

### Gotenberg Client

The Gotenberg client includes:
- **HTTP client** with configurable timeout
- **Retry logic** with exponential backoff
- **Circuit breaker** pattern to prevent cascade failures

### PDF Options

Configurable per-template:
- **Page Size**: A4, A3, Letter, Legal
- **Orientation**: Portrait, Landscape
- **Margins**: Top, Bottom, Left, Right (in mm)
- **Scale**: 0.1 to 2.0
- **Header/Footer**: Optional HTML templates

## Permissions (RBAC)

| Endpoint | Required Permission |
|----------|---------------------|
| Template CRUD | `documents:templates:manage` (manager+) |
| PDF Generation | `documents:generate` (all authenticated) |
| PDF Download | `documents:download` (all authenticated) |

## Best Practices

### Template Design

1. **Use semantic HTML**: Proper heading hierarchy, tables for data
2. **Inline CSS or `<style>` tag**: External CSS is not supported
3. **Test with preview**: Use `/preview` endpoint before generating PDF
4. **Handle missing data**: Use `{{if .field}}` for optional fields
5. **Print-friendly CSS**: Use `@page` rules for margins and page breaks

### Performance

1. **Keep templates simple**: Complex CSS can slow rendering
2. **Optimize images**: Use data URIs sparingly
3. **Batch operations**: Generate multiple PDFs in parallel if needed
4. **Monitor Gotenberg**: Check `/status` endpoint for availability

### Error Handling

The module uses specific error types:
- `ErrTemplateNotFound`: Template UUID doesn't exist
- `ErrTemplateParseError`: Invalid Go template syntax
- `ErrGotenbergUnavailable`: Gotenberg service is down
- `ErrCircuitBreakerOpen`: Too many failures, service temporarily blocked

## MongoDB Collections

### `document_templates`

```javascript
{
  uuid: "...",
  name: "Invoice - Default",
  description: "Default Italian invoice template",
  type: "invoice",
  htmlContent: "<!DOCTYPE html>...",
  cssContent: "body { ... }",
  pageSize: "A4",
  orientation: "portrait",
  margins: { top: 20, bottom: 20, left: 20, right: 20 },
  headerHtml: "",
  footerHtml: "",
  isDefault: true,
  isBuiltIn: true,
  isActive: true,
  version: 1,
  createdAt: ISODate(),
  updatedAt: ISODate()
}
```

### `document_outputs`

```javascript
{
  uuid: "...",
  sourceType: "invoice",
  sourceUuid: "invoice-uuid",
  templateUuid: "template-uuid",
  fileName: "fattura_001.pdf",
  fileSize: 45678,
  contentType: "application/pdf",
  pdfContent: BinData(),  // Binary PDF
  generatedAt: ISODate(),
  generatedBy: "user-uuid",
  createdAt: ISODate()
}
```

## Integration with Billing

The billing module can use this service for generating invoice PDFs:

```go
// In invoice service
func (s *invoiceService) GetInvoicePDF(ctx context.Context, uuid string) (*GeneratedDocument, error) {
    invoice, err := s.GetInvoice(ctx, uuid)
    if err != nil {
        return nil, err
    }

    // Convert invoice to template data
    data := s.invoiceToTemplateData(invoice)

    return s.pdfService.GenerateInvoicePDF(ctx, data, "", userID)
}
```

---

### Related Guides

- [Backend Overview](../../../CLAUDE.md) - Backend architecture
- [Billing Module](../billing/CLAUDE.md) - Invoice generation integration
- [Docker Infrastructure](../../../docker/CLAUDE.md) - Gotenberg service setup
