package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	userServices "github.com/orkestra/backend/internal/core/user/services"
	"github.com/orkestra/backend/internal/shared/blob"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// AvatarHandler hosts the three self-service avatar endpoints under
// `/v1/me/avatar/...`. The handler is tier-bound — the caller wires
// one instance per tier (operator + client) with the matching
// UserService, then mounts each on its own audience mux. The blob
// store is shared across tiers (one bucket, key prefixes keep tiers
// apart).
//
// The pipeline is split into three calls so the SPA can PUT the
// image bytes directly to S3-compatible storage without proxying
// them through the backend:
//
//  1. POST /v1/me/avatar/presign-upload — returns a short-lived PUT
//     URL + the object key the backend chose.
//  2. SPA does `fetch(url, {method:'PUT', headers, body: file})`
//     against S3 directly.
//  3. POST /v1/me/avatar/commit — backend HEADs the object to confirm
//     the upload landed, sets AvatarSource=uploaded + AvatarObjectKey,
//     and GCs the previously-uploaded object (if any).
//
// PATCH /v1/me/avatar/source is the non-upload path: pick an OAuth
// provider already linked to the account, or reset to initials. Both
// paths converge on UserService.SetAvatarSource which validates the
// OAuth-linked invariant.
type AvatarHandler struct {
	svc    userServices.UserService
	store  blob.Store
	tier   string // "operator" or "client" — embedded in object keys
	upload *blob.UploadController
}

// NewAvatarHandler wires the handler. svc must be the tier's UserService;
// store may be nil — when nil the upload endpoints return 503
// storage_unavailable but the source-switch endpoint (oauth_* / initials)
// keeps working. The presign/commit flow (mime allowlist, 2 MiB cap,
// caller-scoped key, commit HEAD-confirm + prior-object GC) is delegated to
// the shared blob.UploadController.
func NewAvatarHandler(svc userServices.UserService, store blob.Store, tier string) *AvatarHandler {
	if tier == "" {
		tier = "operator"
	}
	h := &AvatarHandler{svc: svc, store: store, tier: tier}
	if store != nil {
		h.upload = blob.NewUploadController(blob.UploadConfig{
			Store: store,
			// PNG / JPEG / WebP cover every modern browser's
			// <canvas>.toBlob output. Anything else (SVG XSS vector, a
			// multi-MB TIFF) is rejected at presign time so the signed URL
			// itself cannot land it.
			AllowedContentTypes: map[string]string{
				"image/png": "png", "image/jpeg": "jpg", "image/webp": "webp",
			},
			MaxBytes:   2 * 1024 * 1024, // 2 MiB
			PresignTTL: 10 * time.Minute,
			// avatars/<tier>/<userUUID>/<uuidv7>.<ext> — UUIDv7 keeps
			// concurrent uploads from the same user from colliding and
			// makes the key unguessable from the userUUID alone.
			KeyBuilder: func(s blob.UploadScope, ext string) string {
				return fmt.Sprintf("avatars/%s/%s/%s.%s", tier, s.Subject, uuid.Must(uuid.NewV7()).String(), ext)
			},
			OnCommit: func(ctx context.Context, s blob.UploadScope, key string) (string, error) {
				return svc.SetAvatarSource(ctx, s.Subject, iface.AvatarSourceUploaded, key)
			},
		})
	}
	return h
}

// --- POST presign-upload ---

type presignUploadRequest struct {
	Body struct {
		ContentType string `json:"contentType" doc:"MIME type of the image; one of image/png, image/jpeg, image/webp" enum:"image/png,image/jpeg,image/webp"`
		SizeBytes   int64  `json:"sizeBytes" doc:"Declared byte length of the upload — used only for the server-side cap check (the signer cannot enforce Content-Length on a presigned PUT)" minimum:"1"`
	}
}

type presignUploadResponse struct {
	Body struct {
		URL       string            `json:"url" doc:"S3-compatible PUT URL; valid for ~10 minutes"`
		Headers   map[string]string `json:"headers" doc:"Headers the SPA must include verbatim on the PUT for the signature to validate"`
		Key       string            `json:"key" doc:"Storage handle the SPA echoes back on the commit call"`
		ExpiresAt time.Time         `json:"expiresAt" doc:"Hard expiry of the signed URL"`
	}
}

// PresignAvatarUpload mints a short-lived signed PUT URL.
// Authorization: caller is the owner (every authed user can manage
// their own avatar). RBAC gate at the route level is RequireGlobal()
// — no permission check needed for self-action.
func (h *AvatarHandler) PresignAvatarUpload(ctx context.Context, req *presignUploadRequest) (*presignUploadResponse, error) {
	if h.upload == nil {
		return nil, huma.NewError(http.StatusServiceUnavailable, "avatar_storage_unavailable",
			&huma.ErrorDetail{Message: "object storage is not configured on this deployment"})
	}
	userUUID, ok := ctxauth.GetUserUUID(ctx)
	if !ok || userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	// Preserve the distinct 400 for a non-positive size (the controller
	// otherwise folds sizeBytes<=0 into ErrTooLarge).
	if req.Body.SizeBytes <= 0 {
		return nil, huma.NewError(http.StatusBadRequest, "avatar_invalid_size",
			&huma.ErrorDetail{Message: "sizeBytes must be positive"})
	}
	presigned, err := h.upload.Presign(ctx, blob.UploadScope{Subject: userUUID}, req.Body.ContentType, req.Body.SizeBytes)
	if err != nil {
		switch {
		case errors.Is(err, blob.ErrContentTypeNotAllowed):
			return nil, huma.NewError(http.StatusBadRequest, "avatar_invalid_content_type",
				&huma.ErrorDetail{Message: "contentType must be image/png, image/jpeg, or image/webp"})
		case errors.Is(err, blob.ErrTooLarge):
			return nil, huma.NewError(http.StatusRequestEntityTooLarge, "avatar_too_large",
				&huma.ErrorDetail{Message: "avatar exceeds the 2 MiB cap"})
		}
		slog.ErrorContext(ctx, "avatar presign failed",
			slog.String("user_uuid", userUUID),
			slog.String("error", err.Error()))
		return nil, huma.Error500InternalServerError("failed to mint upload URL", err)
	}
	out := &presignUploadResponse{}
	out.Body.URL = presigned.URL
	out.Body.Headers = presigned.Headers
	out.Body.Key = presigned.Key
	out.Body.ExpiresAt = presigned.ExpiresAt
	return out, nil
}

// --- POST commit ---

type commitAvatarRequest struct {
	Body struct {
		Key string `json:"key" doc:"Object key returned by the presign-upload call after the SPA's direct PUT succeeded" minLength:"1"`
	}
}

type commitAvatarResponse struct {
	Body iface.UserManagementResponse
}

// CommitAvatarUpload promotes a freshly-uploaded blob to be the
// user's active avatar. HEADs the object to confirm the SPA actually
// landed the bytes (a key that doesn't exist returns 404 so the SPA
// can retry the PUT). Side effect: deletes the previously-stored
// object key so the bucket doesn't accumulate orphans on every
// upload.
func (h *AvatarHandler) CommitAvatarUpload(ctx context.Context, req *commitAvatarRequest) (*commitAvatarResponse, error) {
	if h.upload == nil {
		return nil, huma.NewError(http.StatusServiceUnavailable, "avatar_storage_unavailable",
			&huma.ErrorDetail{Message: "object storage is not configured on this deployment"})
	}
	userUUID, ok := ctxauth.GetUserUUID(ctx)
	if !ok || userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	// The controller re-validates the key belongs to this user (prefix
	// avatars/<tier>/<userUUID>/), HEAD-confirms the object landed, calls
	// OnCommit (SetAvatarSource → previous key), and GCs the prior object.
	if err := h.upload.Commit(ctx, blob.UploadScope{Subject: userUUID}, req.Body.Key); err != nil {
		switch {
		case errors.Is(err, blob.ErrKeyOutOfScope):
			return nil, huma.NewError(http.StatusBadRequest, "avatar_key_mismatch",
				&huma.ErrorDetail{Message: "key does not belong to the calling user"})
		case errors.Is(err, blob.ErrUploadNotFound):
			return nil, huma.NewError(http.StatusNotFound, "avatar_blob_missing",
				&huma.ErrorDetail{Message: "uploaded object not found in storage — retry the PUT"})
		}
		return nil, mapAvatarError(err)
	}
	resp, err := h.svc.GetUser(ctx, userUUID)
	if err != nil {
		return nil, mapAvatarError(err)
	}
	// svc.GetUser pipes through enrichWithOAuthProviders which rebuilds
	// Avatar from AvatarSource — no extra resolve needed here.
	return &commitAvatarResponse{Body: *resp}, nil
}

// --- PATCH source ---

type setAvatarSourceRequest struct {
	Body struct {
		Source string `json:"source" doc:"New avatar source. oauth_* requires the matching provider to be linked." enum:"initials,oauth_google,oauth_apple,oauth_github,oauth_discord"`
	}
}

type setAvatarSourceResponse struct {
	Body iface.UserManagementResponse
}

// SetAvatarSource switches the avatar to initials or to a linked
// OAuth provider's picture. Cannot be used to switch to "uploaded"
// — the upload flow goes through presign + commit so the backend
// can verify the bytes landed. Deletes any prior uploaded blob so
// the bucket doesn't accumulate orphans.
func (h *AvatarHandler) SetAvatarSource(ctx context.Context, req *setAvatarSourceRequest) (*setAvatarSourceResponse, error) {
	userUUID, ok := ctxauth.GetUserUUID(ctx)
	if !ok || userUUID == "" {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	source := strings.TrimSpace(req.Body.Source)
	switch source {
	case iface.AvatarSourceInitials,
		iface.AvatarSourceOAuthGoogle,
		iface.AvatarSourceOAuthApple,
		iface.AvatarSourceOAuthGitHub,
		iface.AvatarSourceOAuthDiscord:
	case iface.AvatarSourceUploaded:
		return nil, huma.NewError(http.StatusBadRequest, "avatar_use_commit",
			&huma.ErrorDetail{Message: "use presign-upload + commit to set source to uploaded"})
	default:
		return nil, huma.NewError(http.StatusBadRequest, "avatar_invalid_source",
			&huma.ErrorDetail{Message: "source must be one of initials, oauth_google, oauth_apple, oauth_github, oauth_discord"})
	}
	previousKey, err := h.svc.SetAvatarSource(ctx, userUUID, source, "")
	if err != nil {
		return nil, mapAvatarError(err)
	}
	if previousKey != "" && h.store != nil {
		if delErr := h.store.Delete(ctx, previousKey); delErr != nil {
			slog.WarnContext(ctx, "failed to delete previous avatar object",
				slog.String("user_uuid", userUUID),
				slog.String("key", previousKey),
				slog.String("error", delErr.Error()))
		}
	}
	resp, err := h.svc.GetUser(ctx, userUUID)
	if err != nil {
		return nil, mapAvatarError(err)
	}
	return &setAvatarSourceResponse{Body: *resp}, nil
}

// mapAvatarError translates UserService errors to Huma HTTP shapes.
func mapAvatarError(err error) error {
	switch {
	case errors.Is(err, userServices.ErrUserNotFound):
		return huma.Error404NotFound("user not found")
	case errors.Is(err, userServices.ErrOAuthProviderNotLinked):
		return huma.NewError(http.StatusUnprocessableEntity, "oauth_provider_not_linked",
			&huma.ErrorDetail{Message: "the selected OAuth provider is not linked to your account"})
	case errors.Is(err, userServices.ErrInvalidAvatarSource):
		return huma.Error400BadRequest("invalid avatar source")
	case errors.Is(err, userServices.ErrInvalidInput):
		return huma.Error400BadRequest("invalid input")
	}
	return huma.Error500InternalServerError("avatar update failed", err)
}
