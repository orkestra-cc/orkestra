package models

import "fmt"

// Lookup type constants
const (
	LookupTypeStart        = "start"
	LookupTypeAdvanced     = "advanced"
	LookupTypeMarketing    = "marketing"
	LookupTypeStakeholders = "stakeholders"
	LookupTypeAML          = "aml"
	LookupTypeFull         = "full"
)

// ValidLookupTypes lists all supported lookup types
var ValidLookupTypes = []string{
	LookupTypeStart,
	LookupTypeAdvanced,
	LookupTypeMarketing,
	LookupTypeStakeholders,
	LookupTypeAML,
	LookupTypeFull,
}

// EnrichmentTypes lists only the paid enrichment types (excludes "start")
var EnrichmentTypes = []string{
	LookupTypeAdvanced,
	LookupTypeMarketing,
	LookupTypeStakeholders,
	LookupTypeAML,
	LookupTypeFull,
}

// IsValidLookupType checks if a lookup type is valid
func IsValidLookupType(t string) bool {
	for _, v := range ValidLookupTypes {
		if v == t {
			return true
		}
	}
	return false
}

// IsEnrichmentType checks if a lookup type is a paid enrichment type
func IsEnrichmentType(t string) bool {
	for _, v := range EnrichmentTypes {
		if v == t {
			return true
		}
	}
	return false
}

// EndpointForType returns the API path for a given lookup type and tax code
func EndpointForType(lookupType, taxCode string) (string, error) {
	switch lookupType {
	case LookupTypeStart:
		return fmt.Sprintf("/IT-start/%s", taxCode), nil
	case LookupTypeAdvanced:
		return fmt.Sprintf("/IT-advanced/%s", taxCode), nil
	case LookupTypeMarketing:
		return fmt.Sprintf("/IT-marketing/%s", taxCode), nil
	case LookupTypeStakeholders:
		return fmt.Sprintf("/IT-stakeholders/%s", taxCode), nil
	case LookupTypeAML:
		return fmt.Sprintf("/IT-aml/%s", taxCode), nil
	case LookupTypeFull:
		return fmt.Sprintf("/IT-full/%s", taxCode), nil
	default:
		return "", fmt.Errorf("unknown lookup type: %s", lookupType)
	}
}
