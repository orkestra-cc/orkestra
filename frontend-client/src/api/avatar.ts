// Self-service avatar pipeline against the client-tier surface. Wraps
// the three /v1/me/avatar/* endpoints mounted by the backend user
// module on the client API. Mirrors the operator-console
// authApi.ts/AvatarSettings pattern but stays on this SPA's stack
// (authedFetch + tokenStore, no RTK Query). Hand-typed, like every
// other wrapper here — the generated-client flow left with #325.

import { authedFetch } from "@/api/authedFetch";
import { readError, type ApiError, type MeResponse } from "@/api/auth";

export type AvatarSource =
  | "initials"
  | "uploaded"
  | "oauth_google"
  | "oauth_apple"
  | "oauth_github"
  | "oauth_discord";

export interface PresignedAvatarUpload {
  url: string;
  headers: Record<string, string>;
  key: string;
  expiresAt: string;
}

// The error SHAPE is @/api/auth's, not a local copy: pages branch on `code`,
// and a second definition that drifts is a real hazard. Only the constructor
// stays here, for the one failure below that is not an API response at all
// (the direct-to-storage PUT, which has no JSON body to read).
function err(message: string, status: number, code?: string): ApiError {
  const e = new Error(message) as ApiError;
  e.status = status;
  if (code) e.code = code;
  return e;
}

// presignAvatarUpload mints a short-lived signed PUT URL the SPA
// uploads to directly, bypassing the backend body. The returned
// `key` round-trips back to commit so the backend can verify the
// blob landed before promoting it.
export async function presignAvatarUpload(input: {
  contentType: string;
  sizeBytes: number;
}): Promise<PresignedAvatarUpload> {
  const res = await authedFetch("/v1/me/avatar/presign-upload", {
    method: "POST",
    body: JSON.stringify(input),
  });
  if (!res.ok) throw await readError(res, "Could not start upload");
  return (await res.json()) as PresignedAvatarUpload;
}

// commitAvatarUpload tells the backend the SPA's PUT to S3 landed.
// Backend HEADs the object, sets AvatarSource=uploaded, returns the
// fresh /me payload (with the resolved presigned GET in `avatar`).
export async function commitAvatarUpload(input: {
  key: string;
}): Promise<MeResponse> {
  const res = await authedFetch("/v1/me/avatar/commit", {
    method: "POST",
    body: JSON.stringify(input),
  });
  if (!res.ok) throw await readError(res, "Could not save upload");
  return (await res.json()) as MeResponse;
}

// setAvatarSource switches the avatar to initials or to a linked
// OAuth provider's picture. The backend rejects oauth_* without a
// matching active OAuthLink with 422 oauth_provider_not_linked.
// "uploaded" is rejected with 400 avatar_use_commit — go through
// presign+commit for that path.
export async function setAvatarSource(input: {
  source: Exclude<AvatarSource, "uploaded">;
}): Promise<MeResponse> {
  const res = await authedFetch("/v1/me/avatar/source", {
    method: "PATCH",
    body: JSON.stringify(input),
  });
  if (!res.ok) throw await readError(res, "Could not change avatar");
  return (await res.json()) as MeResponse;
}

// putAvatarBlob uploads the bytes directly to S3-compatible storage.
// Separate from the API wrappers because the URL is signed with no
// auth header and the body is binary, not JSON. Returns void on
// success — the caller chains to commitAvatarUpload.
//
// This is the ONLY raw `fetch` left in this file and it stays that way
// deliberately: the target is a foreign origin (the object store), the URL
// carries its own signature, and `credentials: 'omit'` is the point — nothing
// of ours may leak to it. authedFetch cannot express this call and must not
// be bent to: it prefixes apiBaseURL, attaches our bearer, and forces
// credentials:'include'. A 401 here is the signature expiring, which no
// token refresh can repair.
export async function putAvatarBlob(
  presigned: PresignedAvatarUpload,
  blob: Blob,
): Promise<void> {
  const res = await fetch(presigned.url, {
    method: "PUT",
    headers: presigned.headers,
    body: blob,
    credentials: "omit",
  });
  if (!res.ok) {
    throw err("Direct upload to storage failed", res.status);
  }
}
