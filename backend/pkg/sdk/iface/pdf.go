package iface

import (
	"context"
	"errors"
)

// PDFRenderer converts a self-contained HTML page to PDF bytes. It is an
// optional platform service (module.ServicePDFRenderer): consumers resolve it
// per request and degrade when absent. Implementations never fetch remote
// resources — every file the page references travels in HTMLDocument.Assets.
type PDFRenderer interface {
	RenderHTML(ctx context.Context, doc HTMLDocument) ([]byte, error)
}

// HTMLDocument is one page to render. HTML must be complete (<html>…); Assets
// maps a relative file name used by the page (e.g. "asset-<id>.png") to its
// bytes.
type HTMLDocument struct {
	HTML   string
	Assets map[string][]byte
	Paper  PaperSpec
}

// PaperSpec is the page geometry. Zero values mean the defaults (A4, 15 mm).
type PaperSpec struct {
	Size           string  `bson:"size,omitempty" json:"size,omitempty"` // "A4" | "Letter"
	Landscape      bool    `bson:"landscape,omitempty" json:"landscape,omitempty"`
	MarginTopMM    float64 `bson:"marginTopMm,omitempty" json:"marginTopMm,omitempty"`
	MarginRightMM  float64 `bson:"marginRightMm,omitempty" json:"marginRightMm,omitempty"`
	MarginBottomMM float64 `bson:"marginBottomMm,omitempty" json:"marginBottomMm,omitempty"`
	MarginLeftMM   float64 `bson:"marginLeftMm,omitempty" json:"marginLeftMm,omitempty"`
}

const defaultMarginMM = 15

// Normalized fills the zero fields with the defaults.
func (p PaperSpec) Normalized() PaperSpec {
	if p.Size == "" {
		p.Size = "A4"
	}
	for _, m := range []*float64{&p.MarginTopMM, &p.MarginRightMM, &p.MarginBottomMM, &p.MarginLeftMM} {
		if *m <= 0 {
			*m = defaultMarginMM
		}
	}
	return p
}

// DimensionsInches returns width, height in inches (orientation applied).
func (p PaperSpec) DimensionsInches() (float64, float64) {
	w, h := 8.27, 11.69 // A4
	if p.Size == "Letter" {
		w, h = 8.5, 11
	}
	if p.Landscape {
		return h, w
	}
	return w, h
}

var (
	// ErrPDFRendererUnavailable: renderer absent, unreachable, timed out or 5xx.
	ErrPDFRendererUnavailable = errors.New("pdf renderer unavailable")
	// ErrPDFRenderFailed: the renderer refused the document (4xx) or answered
	// with something that is not a PDF.
	ErrPDFRenderFailed = errors.New("pdf render failed")
	// ErrPDFTooLarge: the produced PDF exceeds the client's size cap.
	ErrPDFTooLarge = errors.New("pdf exceeds size limit")
)
