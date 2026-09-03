import { readError } from "@/api/auth";
import { authedFetch } from "@/api/authedFetch";

// DSR self-service calls (GDPR Art. 15 / 17). These hit core compliance
// endpoints that are not in the generated `paths` type, so they are
// hand-typed — but they go through the same authedFetch as every other
// authenticated call, so they inherit the 401 recovery rather than
// re-deriving it. The error shape is auth.ts's ApiError, imported rather
// than copied: pages branch on `code`, and a private copy is how that drifts.
//
// /v1/me/dsr/export takes no body (its Huma handler declares `_ *struct{}`),
// so nothing sets Content-Type for it — correctly: a content type without
// content describes nothing.
async function postJson<T>(path: string, body?: unknown): Promise<T> {
  const res = await authedFetch(path, {
    method: "POST",
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) throw await readError(res, "Request failed");
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
