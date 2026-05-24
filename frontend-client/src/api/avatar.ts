// Self-service avatar pipeline against the client-tier surface. Wraps
// the three /v1/me/avatar/* endpoints mounted by the backend user
// module on the client API. Mirrors the operator-console
// authApi.ts/AvatarSettings pattern but stays on this SPA's stack
// (raw fetch + tokenStore, no RTK Query). Hand-typed for now —
// codegen will pick these up after the next `npm run codegen`.

import { apiBaseURL } from "@/api/client";
import { getAccessToken } from "@/auth/tokenStore";
import type { MeResponse } from "@/api/auth";

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

interface ApiError extends Error {
  status: number;
  code?: string;
}

function err(message: string, status: number, code?: string): ApiError {
  const e = new Error(message) as ApiError;
  e.status = status;
  if (code) e.code = code;
  return e;
}

async function readError(res: Response, fallback: string): Promise<ApiError> {
  const body = (await res.json().catch(() => ({}))) as {
    detail?: string;
    title?: string;
    code?: string;
  };
  return err(body.detail ?? body.title ?? fallback, res.status, body.code);
}

async function authedJson(path: string, init?: RequestInit): Promise<Response> {
  const token = getAccessToken();
  return fetch(`${apiBaseURL}${path}`, {
    credentials: "include",
    ...init,
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json",
      ...(init?.headers ?? {}),
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
  });
}

// presignAvatarUpload mints a short-lived signed PUT URL the SPA
// uploads to directly, bypassing the backend body. The returned
// `key` round-trips back to commit so the backend can verify the
// blob landed before promoting it.
export async function presignAvatarUpload(input: {
  contentType: string;
  sizeBytes: number;
}): Promise<PresignedAvatarUpload> {
  const res = await authedJson("/v1/me/avatar/presign-upload", {
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
  const res = await authedJson("/v1/me/avatar/commit", {
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
  const res = await authedJson("/v1/me/avatar/source", {
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
