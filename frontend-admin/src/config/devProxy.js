// Same-origin dev proxy for the Vite dev server. When VITE_DEV_PROXY_TARGET
// is set, /v1 (HTTP) and /ws (WebSocket) are forwarded to that backend, so
// the console can call its API on its own origin. Unset or empty, the proxy
// is off and the dev and staging stacks behave exactly as before.
//
// `storage` (optional): for a dev stack whose object store the browser cannot
// reach. The backend signs its presigned URLs for the dev server's own origin
// (STORAGE_PUBLIC_ENDPOINT) and the dev server forwards the path-style bucket
// paths `/<prefix>-<domain>/…` to the store. The Host header is not rewritten
// (no changeOrigin): the signature covers it, so the store must see the host
// the browser signed for. A prefix that is not a plain bucket prefix adds
// nothing.
const BUCKET_PREFIX = /^[a-z0-9][a-z0-9-]{0,40}$/;

export const devProxy = (target, storage) => {
  if (!target) return undefined;
  const proxy = {
    '/v1': { target },
    '/ws': { target, ws: true }
  };
  if (storage?.target && BUCKET_PREFIX.test(storage.prefix ?? '')) {
    proxy[`^/${storage.prefix}-[a-z0-9-]+/`] = { target: storage.target };
  }
  return proxy;
};
