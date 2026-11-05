// Shared binary-download helper for RTK Query slices.
//
// A file endpoint (CSV/XLSX export, PDF, ...) cannot be a plain `query` — it
// needs a `responseHandler` to read the response as a Blob instead of JSON —
// so it must be a `queryFn` that calls the slice's OWN injected `baseQuery`
// rather than a hand-rolled `fetch`. `baseQueryWithRetry` (baseApi.ts) is the
// single owner of X-Tenant-ID (org switcher + impersonation), the
// Bearer/cookie pair and both refresh paths; a raw `fetch` sends only
// Authorization, so the request still succeeds but the backend falls back to
// the token's own fallback tenant instead of the org selected in the console.
// That bug has shipped more than once in binary endpoints (invoice PDFs,
// import wizards, CSV exports), always for the same reason.
//
// Every binary endpoint should reuse this instead of re-deriving the fix:
//
//   exportThings: builder.mutation<{ success: true }, ExportArgs>({
//     queryFn: (args, _api, _extra, baseQuery) =>
//       fileDownload(
//         baseQuery,
//         { url: '/v1/things/export', method: 'POST', body: args },
//         datedFilename('things')
//       )
//   })
import type {
  FetchArgs,
  FetchBaseQueryError,
  FetchBaseQueryMeta,
  QueryReturnValue
} from '@reduxjs/toolkit/query';
import { saveBlob } from './saveBlob';

// The `baseQuery` handed to a queryFn as its fourth argument: already bound to
// this slice's api + extraOptions, so it takes the request and nothing else.
export type CsvQueryResult = QueryReturnValue<
  unknown,
  FetchBaseQueryError,
  FetchBaseQueryMeta
>;
export type CsvBaseQuery = (
  arg: FetchArgs
) => CsvQueryResult | PromiseLike<CsvQueryResult>;

// fetchBaseQuery applies the SAME responseHandler (here: `.blob()`) to an
// error response as to a success — so a 403/422 error body (an errcode.Error
// JSON) comes back as an opaque Blob in `error.data`, not the parsed
// {status,title,detail,code} the backend sent. A caller reading `.data.code`
// to tell one failure from another would silently see nothing. Recover it:
// per the Fetch spec a Blob's `type` is the Content-Type essence, so a JSON
// error blob is detectable and re-parsed back into its structured body.
export const asStructuredError = async (
  error: FetchBaseQueryError
): Promise<FetchBaseQueryError> => {
  const data = (error as { data?: unknown }).data;
  if (data instanceof Blob && data.type.startsWith('application/json')) {
    try {
      return { ...error, data: JSON.parse(await data.text()) };
    } catch {
      // Not actually JSON despite the content-type: leave the Blob as-is.
    }
  }
  return error;
};

// Fetch a file stream through baseQuery and hand it to the Save dialog. The
// Blob stays out of the store: only the marker below is returned. `method`
// + `body` let a POST export send its format/ids/filters selection.
export const fileDownload = async (
  baseQuery: CsvBaseQuery,
  req: { url: string; method?: 'GET' | 'POST'; body?: unknown },
  filename: string
): Promise<{ data: { success: true } } | { error: FetchBaseQueryError }> => {
  const result = await baseQuery({
    url: req.url,
    method: req.method ?? 'GET',
    body: req.body,
    responseHandler: response => response.blob()
  });
  if (result.error) {
    return { error: await asStructuredError(result.error) };
  }
  try {
    saveBlob(result.data as Blob, filename);
    return { data: { success: true as const } };
  } catch (error) {
    return { error: { status: 'CUSTOM_ERROR', error: String(error) } };
  }
};
