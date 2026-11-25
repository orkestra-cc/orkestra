// Hand a downloaded Blob to the browser's Save dialog.
//
// Lives outside the api slices so a binary endpoint can be a `queryFn` that
// goes through the injected `baseQuery` — the only place X-Tenant-ID, the
// Bearer/cookie pair and the refresh paths are attached — while the Blob
// itself never enters the RTK Query cache: the endpoint returns a
// serializable marker and this function performs the side effect.
export const saveBlob = (blob: Blob, filename: string): void => {
  const url = window.URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  window.URL.revokeObjectURL(url);
};

// `<stem>-YYYY-MM-DD.csv` — a dated name, so exports from different days stay apart.
export const datedFilename = (stem: string, ext = 'csv'): string =>
  `${stem}-${new Date().toISOString().slice(0, 10)}.${ext}`;
