import { LLM_FIXED_BASE_URL, type LlmProvider } from 'store/api/llmApi';

// Client-side mirrors of backend/internal/core/llm/models rules, for early
// feedback only — the backend stays authoritative and answers
// llm.invalid_request with a written detail when these drift.

// models.nameRE
export const LLM_NAME_RE = /^[\p{L}\p{N}][\p{L}\p{N} ._-]{0,63}$/u;
// models.modelIDRE
export const LLM_MODEL_ID_RE = /^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$/;
// models.purposeRE
export const LLM_PURPOSE_RE = /^[a-z][a-z0-9_.-]{0,63}$/;

// models.NeedsSecret
export const needsSecret = (p: LlmProvider): boolean =>
  p !== 'ollama' && p !== 'mock';

// models.IsHostedProvider: cloud providers behind the allow_hosted opt-in.
export const isHostedProvider = (p: LlmProvider): boolean =>
  p === 'openai' ||
  p === 'anthropic' ||
  p === 'gemini' ||
  p === 'openai_compatible';

// Providers whose endpoint the operator types (no fixed base URL, not mock).
export const needsBaseUrl = (p: LlmProvider): boolean =>
  !LLM_FIXED_BASE_URL[p] && p !== 'mock';

// An absolute http(s) URL. Deliberately looser than yup's .url(), which
// rejects a host without a TLD such as the compose-internal
// http://ollama:11434; endpoint safety is the backend's decision.
export const isHttpUrl = (value: string): boolean => {
  try {
    const u = new URL(value);
    return (u.protocol === 'http:' || u.protocol === 'https:') && !!u.hostname;
  } catch {
    return false;
  }
};
