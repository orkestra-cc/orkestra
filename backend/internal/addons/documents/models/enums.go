package models

// TemplateType represents the type of document template
type TemplateType string

const (
	// TemplateTypeInvoice is for invoice documents
	TemplateTypeInvoice TemplateType = "invoice"
	// TemplateTypeOffer is for quote/offer documents
	TemplateTypeOffer TemplateType = "offer"
	// TemplateTypeReceipt is for receipt documents
	TemplateTypeReceipt TemplateType = "receipt"
	// TemplateTypeCustom is for custom document templates
	TemplateTypeCustom TemplateType = "custom"
)

// ValidTemplateTypes returns all valid template types
func ValidTemplateTypes() []TemplateType {
	return []TemplateType{
		TemplateTypeInvoice,
		TemplateTypeOffer,
		TemplateTypeReceipt,
		TemplateTypeCustom,
	}
}

// IsValid checks if the template type is valid
func (t TemplateType) IsValid() bool {
	switch t {
	case TemplateTypeInvoice, TemplateTypeOffer, TemplateTypeReceipt, TemplateTypeCustom:
		return true
	}
	return false
}

// String returns the string representation
func (t TemplateType) String() string {
	return string(t)
}

// PageSize represents the paper size for PDF generation
type PageSize string

const (
	// PageSizeA4 is the standard A4 paper size (210mm x 297mm)
	PageSizeA4 PageSize = "A4"
	// PageSizeA3 is the A3 paper size (297mm x 420mm)
	PageSizeA3 PageSize = "A3"
	// PageSizeLetter is the US Letter size (215.9mm x 279.4mm)
	PageSizeLetter PageSize = "Letter"
	// PageSizeLegal is the US Legal size (215.9mm x 355.6mm)
	PageSizeLegal PageSize = "Legal"
)

// ValidPageSizes returns all valid page sizes
func ValidPageSizes() []PageSize {
	return []PageSize{
		PageSizeA4,
		PageSizeA3,
		PageSizeLetter,
		PageSizeLegal,
	}
}

// IsValid checks if the page size is valid
func (p PageSize) IsValid() bool {
	switch p {
	case PageSizeA4, PageSizeA3, PageSizeLetter, PageSizeLegal:
		return true
	}
	return false
}

// String returns the string representation
func (p PageSize) String() string {
	return string(p)
}

// PageOrientation represents the page orientation
type PageOrientation string

const (
	// PageOrientationPortrait is the vertical orientation
	PageOrientationPortrait PageOrientation = "portrait"
	// PageOrientationLandscape is the horizontal orientation
	PageOrientationLandscape PageOrientation = "landscape"
)

// ValidPageOrientations returns all valid page orientations
func ValidPageOrientations() []PageOrientation {
	return []PageOrientation{
		PageOrientationPortrait,
		PageOrientationLandscape,
	}
}

// IsValid checks if the page orientation is valid
func (o PageOrientation) IsValid() bool {
	switch o {
	case PageOrientationPortrait, PageOrientationLandscape:
		return true
	}
	return false
}

// String returns the string representation
func (o PageOrientation) String() string {
	return string(o)
}

// SourceType (and its constants/methods) lives in shared/iface/document_types.go
// — see iface.SourceType, iface.SourceTypeInvoice, etc. Kept out of this
// addon package so the iface contract layer doesn't import addon code.
