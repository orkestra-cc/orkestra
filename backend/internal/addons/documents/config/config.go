package config

import "time"

// Config holds configuration for the documents module
type Config struct {
	// Gotenberg service configuration
	GotenbergURL   string        // Gotenberg service URL (e.g., http://gotenberg:3000)
	Timeout        time.Duration // HTTP client timeout for PDF generation
	RetryAttempts  int           // Number of retry attempts for failed requests
	DefaultMargins PDFMargins    // Default page margins in millimeters
}

// PDFMargins defines page margins for PDF generation
type PDFMargins struct {
	Top    float64 // Top margin in millimeters
	Bottom float64 // Bottom margin in millimeters
	Left   float64 // Left margin in millimeters
	Right  float64 // Right margin in millimeters
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		GotenbergURL:  "http://gotenberg:3000",
		Timeout:       60 * time.Second,
		RetryAttempts: 3,
		DefaultMargins: PDFMargins{
			Top:    20.0,
			Bottom: 20.0,
			Left:   20.0,
			Right:  20.0,
		},
	}
}

// PDFOptions holds options for PDF generation
type PDFOptions struct {
	PageSize    string     // A4, A3, Letter, Legal
	Orientation string     // portrait, landscape
	Margins     PDFMargins // Page margins in mm
	HeaderHTML  string     // Optional header HTML
	FooterHTML  string     // Optional footer HTML
	Scale       float64    // Scale factor (0.1 to 2.0)
}

// DefaultPDFOptions returns default PDF options
func DefaultPDFOptions() *PDFOptions {
	return &PDFOptions{
		PageSize:    "A4",
		Orientation: "portrait",
		Margins: PDFMargins{
			Top:    20.0,
			Bottom: 20.0,
			Left:   20.0,
			Right:  20.0,
		},
		Scale: 1.0,
	}
}
