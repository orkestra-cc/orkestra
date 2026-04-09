package services

import "context"

// PDFGenerator is the consumer-defined interface for PDF generation.
// Satisfied by the documents module's PDF service via Gotenberg.
type PDFGenerator interface {
	GeneratePDF(ctx context.Context, htmlContent string) ([]byte, error)
}
