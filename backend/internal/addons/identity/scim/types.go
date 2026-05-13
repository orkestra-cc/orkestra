// Package scim defines the SCIM 2.0 wire shapes used by the identity
// module's stub endpoints. Phase-3.4 of the tenancy plan lands these
// shapes so IdPs (Okta, Entra, JumpCloud, OneLogin, …) can point their
// SCIM provisioning clients at Orkestra and receive RFC-7644-compliant
// responses, even while persistence is not yet implemented.
//
// v1 behavior:
//   - Metadata endpoints (ServiceProviderConfig, Schemas, ResourceTypes)
//     return fully-populated static documents.
//   - /Users and /Groups return empty ListResponse on GET.
//   - All mutations return 501 with a SCIM Error envelope.
//
// When full provisioning lands (future commit), the same types are reused
// — only the handlers switch from stub responses to persistence calls.
package scim

// Core schema URIs (RFC 7643).
const (
	SchemaListResponse          = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	SchemaError                 = "urn:ietf:params:scim:api:messages:2.0:Error"
	SchemaUser                  = "urn:ietf:params:scim:schemas:core:2.0:User"
	SchemaGroup                 = "urn:ietf:params:scim:schemas:core:2.0:Group"
	SchemaServiceProviderConfig = "urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"
	SchemaResourceType          = "urn:ietf:params:scim:schemas:core:2.0:ResourceType"
	SchemaEnterpriseUser        = "urn:ietf:params:scim:schemas:extension:enterprise:2.0:User"
)

// ListResponse is the envelope every list endpoint returns, even when
// empty (SCIM clients special-case the absence of Resources).
type ListResponse struct {
	Schemas      []string `json:"schemas"`
	TotalResults int      `json:"totalResults"`
	StartIndex   int      `json:"startIndex,omitempty"`
	ItemsPerPage int      `json:"itemsPerPage,omitempty"`
	Resources    []any    `json:"Resources"`
}

// EmptyList returns a properly-formed empty ListResponse.
func EmptyList() ListResponse {
	return ListResponse{
		Schemas:      []string{SchemaListResponse},
		TotalResults: 0,
		StartIndex:   1,
		ItemsPerPage: 0,
		Resources:    []any{},
	}
}

// Error is the SCIM error envelope (RFC 7644 §3.12). Status mirrors the
// HTTP status as a string; ScimType narrows specific validation failures.
type Error struct {
	Schemas  []string `json:"schemas"`
	Status   string   `json:"status"`
	ScimType string   `json:"scimType,omitempty"`
	Detail   string   `json:"detail,omitempty"`
}

// NewError constructs a SCIM error with the given HTTP-status string.
func NewError(status, detail string) Error {
	return Error{
		Schemas: []string{SchemaError},
		Status:  status,
		Detail:  detail,
	}
}

// --- ServiceProviderConfig ---

// ServiceProviderConfig describes which SCIM features the server supports.
// Stubbed: patch/bulk/filter/etag/sort are all disabled in v1.
type ServiceProviderConfig struct {
	Schemas               []string               `json:"schemas"`
	DocumentationURI      string                 `json:"documentationUri,omitempty"`
	Patch                 SupportedFeature       `json:"patch"`
	Bulk                  SupportedBulk          `json:"bulk"`
	Filter                SupportedFilter        `json:"filter"`
	ChangePassword        SupportedFeature       `json:"changePassword"`
	Sort                  SupportedFeature       `json:"sort"`
	ETag                  SupportedFeature       `json:"etag"`
	AuthenticationSchemes []AuthenticationScheme `json:"authenticationSchemes"`
	Meta                  *Meta                  `json:"meta,omitempty"`
}

type SupportedFeature struct {
	Supported bool `json:"supported"`
}

type SupportedBulk struct {
	Supported      bool `json:"supported"`
	MaxOperations  int  `json:"maxOperations,omitempty"`
	MaxPayloadSize int  `json:"maxPayloadSize,omitempty"`
}

type SupportedFilter struct {
	Supported  bool `json:"supported"`
	MaxResults int  `json:"maxResults,omitempty"`
}

// AuthenticationScheme describes a way a SCIM client can authenticate.
// v1 advertises only "oauthbearertoken" — each tenant holds a single
// long-lived bearer token minted via the admin rotate endpoint.
type AuthenticationScheme struct {
	Type             string `json:"type"`
	Name             string `json:"name"`
	Description      string `json:"description"`
	SpecURI          string `json:"specUri,omitempty"`
	DocumentationURI string `json:"documentationUri,omitempty"`
	Primary          bool   `json:"primary,omitempty"`
}

// --- ResourceType ---

// ResourceType documents one of the resources the server hosts
// (v1: User, Group). Returned by /ResourceTypes.
type ResourceType struct {
	Schemas     []string `json:"schemas"`
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Endpoint    string   `json:"endpoint"`
	Description string   `json:"description,omitempty"`
	Schema      string   `json:"schema"`
	Meta        *Meta    `json:"meta,omitempty"`
}

// --- Schema ---

// Schema is the full schema document for a resource, returned by /Schemas.
// The stub ships core User + Group — extending to the enterprise schema
// is a follow-up.
type Schema struct {
	Schemas     []string          `json:"schemas"`
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Attributes  []SchemaAttribute `json:"attributes"`
	Meta        *Meta             `json:"meta,omitempty"`
}

// SchemaAttribute is a single attribute declaration within a Schema.
type SchemaAttribute struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	CaseExact   bool   `json:"caseExact,omitempty"`
	Mutability  string `json:"mutability,omitempty"`
	Returned    string `json:"returned,omitempty"`
	Uniqueness  string `json:"uniqueness,omitempty"`
	MultiValued bool   `json:"multiValued,omitempty"`
	Description string `json:"description,omitempty"`
}

// Meta is the common metadata envelope attached to every resource.
type Meta struct {
	ResourceType string `json:"resourceType,omitempty"`
	Location     string `json:"location,omitempty"`
	Version      string `json:"version,omitempty"`
	Created      string `json:"created,omitempty"`
	LastModified string `json:"lastModified,omitempty"`
}

// --- User + Group (minimal shapes, not yet persisted) ---

// User is the SCIM core User resource. v1 handlers never return a
// populated User — but downstream libraries still expect the struct
// to exist so typed clients compile against our OpenAPI spec.
type User struct {
	Schemas    []string  `json:"schemas"`
	ID         string    `json:"id,omitempty"`
	ExternalID string    `json:"externalId,omitempty"`
	UserName   string    `json:"userName"`
	Name       *UserName `json:"name,omitempty"`
	Emails     []Email   `json:"emails,omitempty"`
	Active     bool      `json:"active"`
	Meta       *Meta     `json:"meta,omitempty"`
}

type UserName struct {
	Formatted  string `json:"formatted,omitempty"`
	FamilyName string `json:"familyName,omitempty"`
	GivenName  string `json:"givenName,omitempty"`
}

type Email struct {
	Value   string `json:"value"`
	Type    string `json:"type,omitempty"`
	Primary bool   `json:"primary,omitempty"`
}

// Group is the SCIM core Group resource.
type Group struct {
	Schemas     []string      `json:"schemas"`
	ID          string        `json:"id,omitempty"`
	DisplayName string        `json:"displayName"`
	Members     []GroupMember `json:"members,omitempty"`
	Meta        *Meta         `json:"meta,omitempty"`
}

type GroupMember struct {
	Value   string `json:"value"`
	Display string `json:"display,omitempty"`
	Type    string `json:"type,omitempty"`
}
