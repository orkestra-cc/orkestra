import { apiBaseURL } from "@/api/client";
import { getAccessToken } from "@/auth/tokenStore";

// DSR self-service calls (GDPR Art. 15 / 17). These hit core compliance
// endpoints that aren't in the generated openapi-fetch `paths` type, so they
// use a small raw-fetch helper rather than the typed `api` client. The bearer
// token + credentials mirror the typed client's middleware.

async function postJson<T>(path: string, body?: unknown): Promise<T> {
  const token = getAccessToken();
  const res = await fetch(`${apiBaseURL}${path}`, {
    method: "POST",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) {
    throw new Error(`request failed (${res.status})`);
  }
  return (await res.json()) as T;
}

export interface ErasureRequestResult {
  uuid: string;
  status: string;
  requestedAt: string;
}

/** exportMyData returns the caller's full personal-data bundle (Art. 15 / 20). */
export function exportMyData(): Promise<unknown> {
  return postJson<unknown>("/v1/me/dsr/export");
}

/** requestErasure lodges a mediated right-to-erasure request (Art. 17). */
export function requestErasure(reason?: string): Promise<ErasureRequestResult> {
  return postJson<ErasureRequestResult>("/v1/me/dsr/erasure-request", {
    reason: reason ?? "",
  });
}
