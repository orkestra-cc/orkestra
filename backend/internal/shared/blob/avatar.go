package blob

import (
	"context"
	"log/slog"
	"time"

	"github.com/orkestra-cc/orkestra-sdk/iface"
)

// PresignedAvatarGetTTL is the validity window on every presigned GET
// URL minted for an uploaded avatar. Kept at 1h so the same URL is
// reusable across page navigations; the CachedStore in front of the
// raw Store further amortizes by ~50 minutes so the SPA's <img> tag
// honors HTTP caching across pages.
const PresignedAvatarGetTTL = time.Hour

// ResolveAvatarURL is the canonical resolver for the rendered avatar
// URL given a User document and an optional blob.Store. Lives in
// the blob package so both the user-module read paths and the auth-
// module GetCurrentUser handler can call it without crossing module
// boundaries.
//
// Source semantics:
//
//   - "uploaded"  → fresh presigned GET on user.AvatarObjectKey; falls
//     back to user.Avatar when store is nil or the presign fails so
//     a degraded deployment still serves the last good URL.
//   - "oauth_*"   → the matching OAuthLink.OAuthData["picture"]; "" when
//     not present (Apple never carries one, unlinked providers were
//     already rejected by the SetAvatarSource service guard).
//   - "initials"  → "" — the SPA renders initials.
//   - empty (legacy) → user.Avatar unchanged; boot-time backfill stamps
//     "initials" so this branch shrinks over time.
//
// store may be nil. Callers without a blob store wired (deploys that
// haven't enabled object storage) get a graceful degradation rather
// than an error.
func ResolveAvatarURL(ctx context.Context, user *iface.User, store Store) string {
	if user == nil {
		return ""
	}
	switch user.AvatarSource {
	case iface.AvatarSourceInitials:
		return ""
	case iface.AvatarSourceUploaded:
		if store == nil || user.AvatarObjectKey == "" {
			return user.Avatar
		}
		url, err := store.PresignGet(ctx, user.AvatarObjectKey, PresignedAvatarGetTTL)
		if err != nil {
			slog.WarnContext(ctx, "avatar presign get failed",
				slog.String("user_uuid", user.UUID),
				slog.String("error", err.Error()))
			return user.Avatar
		}
		return url
	case iface.AvatarSourceOAuthGoogle:
		return oauthLinkPicture(user, iface.OAuthProviderGoogle)
	case iface.AvatarSourceOAuthApple:
		return oauthLinkPicture(user, iface.OAuthProviderApple)
	case iface.AvatarSourceOAuthGitHub:
		return oauthLinkPicture(user, iface.OAuthProviderGitHub)
	case iface.AvatarSourceOAuthDiscord:
		return oauthLinkPicture(user, iface.OAuthProviderDiscord)
	}
	return user.Avatar
}

// oauthLinkPicture extracts the cached `picture` URL from the user's
// embedded OAuth links for the requested provider. Returns "" when
// the link is missing, inactive, or doesn't carry a picture.
func oauthLinkPicture(user *iface.User, provider iface.OAuthProvider) string {
	for _, link := range user.OAuthLinks {
		if link.Provider != provider || !link.IsActive {
			continue
		}
		if pic, ok := link.OAuthData["picture"].(string); ok {
			return pic
		}
	}
	return ""
}
