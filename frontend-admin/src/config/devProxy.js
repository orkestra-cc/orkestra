// Same-origin dev proxy for the Vite dev server. When VITE_DEV_PROXY_TARGET
// is set, /v1 (HTTP) and /ws (WebSocket) are forwarded to that backend, so
// the console can call its API on its own origin. Unset or empty, the proxy
// is off and the dev and staging stacks behave exactly as before.
export const devProxy = target => {
  if (!target) return undefined;
  return {
    '/v1': { target },
    '/ws': { target, ws: true }
  };
};
