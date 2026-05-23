// Orkestra Identity addon — per-tenant Bring-Your-Own-Identity-Provider
// (OIDC) login + SCIM 2.0 provisioning. Owns `identity_idp_configs`
// (the per-tenant OIDC configuration registry) and `identity_scim_tokens`
// (the bearer tokens that SCIM agents present), serves the public
// `/v1/identity/oidc/{tenantSlug}/start` + `/callback` flow, exposes
// tenant-admin CRUD at `/v1/admin/identity/oidc`, and mounts the SCIM 2.0
// surface at `/v1/scim/v2/`. Bridges OIDC authentication into Orkestra
// sessions via the SDK's `iface.LoginTokenIssuer` contract (implemented
// by core/auth's PasswordAuthService.IssueLoginTokensExternal), so the
// addon depends on no backend-internal auth types. Hosted in-tree at
// backend/internal/addons/identity/ as its own Go module so the same
// source can be consumed by the orkestra monolith AND extracted to a
// standalone repository (orkestra-cc/orkestra-addon-identity) at the
// same import path. The in-tree path is intentionally still under
// backend/internal/ — Go's internal-package rule operates on import
// paths, not filesystem locations, and the import path here does NOT
// contain "internal".

module github.com/orkestra-cc/orkestra-addon-identity

go 1.25.10

require (
	github.com/coreos/go-oidc/v3 v3.18.0
	github.com/danielgtaylor/huma/v2 v2.34.1
	github.com/go-chi/chi/v5 v5.2.5
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/google/uuid v1.6.0
	github.com/orkestra-cc/orkestra-sdk v0.4.0
	go.mongodb.org/mongo-driver v1.17.6
	golang.org/x/oauth2 v0.36.0
)

require (
	github.com/go-jose/go-jose/v4 v4.1.4 // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/klauspost/compress v1.18.1 // indirect
	github.com/montanaflynn/stats v0.7.1 // indirect
	github.com/xdg-go/pbkdf2 v1.0.0 // indirect
	github.com/xdg-go/scram v1.1.2 // indirect
	github.com/xdg-go/stringprep v1.0.4 // indirect
	github.com/youmark/pkcs8 v0.0.0-20240726163527-a2c0da244d78 // indirect
	golang.org/x/crypto v0.52.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/text v0.37.0 // indirect
)
