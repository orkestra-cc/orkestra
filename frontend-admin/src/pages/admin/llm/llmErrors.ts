// Error helpers shared by the /admin/llm tabs and editors.

interface ApiError {
  data?: { code?: string; detail?: string };
}

const codeOf = (err: unknown): string | undefined =>
  (err as ApiError | undefined)?.data?.code;

// A step-up or password re-confirmation the operator dismissed. baseApi's
// 401 interceptor already showed the prompt and returns the original 401
// when it is cancelled, so the caller says nothing more (precedent:
// pages/user/security/LinkedProvidersTab.tsx).
export const isReauthCancelled = (err: unknown): boolean => {
  const code = codeOf(err);
  return code === 'step_up_required' || code === 'password_confirm_required';
};

// llmErrorMessage prefers the translated `errors.<code>` copy (the backend's
// llm.* codes live nested under errors.llm), then the backend's written
// detail, then a generic line — the console convention of
// hooks/ui/useUserTable.tsx.
export const llmErrorMessage = (
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
  return t('adminLlm.errors.generic');
};
