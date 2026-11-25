// Error helpers shared by the /admin/retrieval tabs.

interface ApiError {
  data?: { code?: string; detail?: string };
}

// A dismissed step-up: baseApi's 401 interceptor already showed the prompt,
// so the caller says nothing more.
export { isReauthCancelled } from 'pages/admin/llm/llmErrors';

// retrievalErrorMessage prefers the translated `errors.<code>` copy (the
// backend's retrieval.* codes live nested under errors.retrieval), then the
// backend's written detail, then a generic line.
export const retrievalErrorMessage = (
  t: (key: string) => string,
  err: unknown
): string => {
  const data = (err as ApiError | undefined)?.data;
  if (data?.code) {
    const key = `errors.${data.code}`;
    const translated = t(key);
    if (translated && translated !== key) return translated;
  }
  if (data?.detail) return data.detail;
  return t('adminRetrieval.errors.generic');
};
