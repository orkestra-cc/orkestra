# Runbook — Cloudflare Tunnel ⇄ direct switch

Scope: a staging or production stack whose public hostnames are proxied by
Cloudflare. `./orkestra.sh tunnel` moves those hostnames between two ways in,
**without changing a single URL**:

| Mode | Path of a request | DNS of each hostname |
| --- | --- | --- |
| **direct** | Cloudflare proxy → origin public IP → reverse proxy (HAProxy, Caddy…) → the ports published on `HOST_BIND_ADDRESS` | proxied A/AAAA/CNAME to the origin |
| **tunnel** | Cloudflare proxy → this stack's `cloudflared` container (outbound-only connection) → the services by name on the stack network | proxied CNAME `<tunnel-id>.cfargotunnel.com` |

The typical use is an incident: the origin's line or its reverse proxy is down
(Cloudflare answers 522), the host still has outbound internet, and the
tunnel brings the site back in a minute. It also works as the permanent mode.

The host ports stay published in both modes, so while the tunnel is up the
direct path keeps working and going back is only a DNS change. Nothing in
`docker/.env` (`FRONTEND_URL`, `BACKEND_URL`, `VITE_*`, CORS, cookie domains)
depends on the mode.

The mechanism was built during such an incident (2026-11-05) and has served
a production stack since. It is generic: a fork adds only its routes file.

## The pieces

- **Routes file** `docker/cloudflared/<ENV>.json` — the single source of
  hostname → service. It is cloudflared's config file (JSON is valid YAML;
  cloudflared ignores the top-level `_note`, `_tunnel_id` and `_smoke`) and
  the exact list of hostnames whose DNS the switch moves. **This repo ships
  no routes**: only `docker/cloudflared/{production,staging}.json.example`. A
  fork copies one to `<ENV>.json`, lists its own hostnames and commits it.
  - `service` uses compose **service** names (`frontend-admin`, `backend`,
    `client-frontend`, `rustfs`), never container names: they resolve on the
    stack network whatever `APP_NAME`/`ENV` are. Use the port the service
    listens on inside the network: in production the console is nginx on
    `frontend-admin:80`; staging runs the Vite dev servers, so the console is
    `frontend-admin:5173` and the client SPA `client-frontend:5173`. Vite
    refuses a `Host` it does not allow (403): list the staging hostnames in
    `VITE_ALLOWED_HOSTS` / `VITE_CLIENT_ALLOWED_HOSTS`, as for the direct path.
  - the last rule must be the catch-all `{"service": "http_status:404"}`.
  - `_smoke` gives the path each hostname is smoke-tested at (`/health`
    answers without authentication on the backend, the console, the client
    SPA's dev server and RustFS).
  - `_tunnel_id` pins the id of **this** environment's tunnel (not a secret:
    it is the CNAME target).
  - check a change with
    `docker run --rm -v "$PWD/docker/cloudflared:/etc/cloudflared:ro" cloudflare/cloudflared:2026.10.7 tunnel --config /etc/cloudflared/production.json ingress validate`.
- **`cloudflared` service** in `docker/docker-compose.prod.yml` and, with the
  same definition, in `docker/docker-compose.staging.yml` (staging runs its own
  full file): compose profile `tunnel`, so it exists only when
  the switch or a tunnel-enabled deploy starts it. Pinned image,
  `read_only`, `no-new-privileges`, all capabilities dropped, no published
  port, `TUNNEL_TOKEN` from `CLOUDFLARE_TUNNEL_TOKEN`, metrics on
  `127.0.0.1:20241` (loopback of its own network namespace: the switch reads
  them from there, nothing else can). It mounts the **directory**
  `docker/cloudflared/` read-only, not the file, so a checkout that replaces
  the routes file is seen at the next restart. Container name
  `${APP_NAME}-cloudflared-${ENV}`.
- **`./orkestra.sh tunnel …`** — the switch (`orkestra.sh`, `tunnel_*`
  functions). It addresses its container only by that exact name and only
  when it carries this stack's compose project label: another cloudflared on
  the host (another stack's, or a dashboard-managed connector) is never
  stopped, replaced or read. One tunnel command runs at a time per stack (`flock` on
  `docker/cloudflared/state/.lock`).
- **DNS snapshot** `docker/cloudflared/state/<ENV>-direct-dns.json`
  (gitignored, per host, mode 700 directory): per hostname
  `{"records": [...], "via_wildcard": true|false}` — the A/AAAA/CNAME records
  of its own (with their `comment` and `tags`) before its **first** switch,
  or `[]` with `via_wildcard: true` for a name that had none and answered
  through a wildcard. An entry is written before the name's first change and
  **never overwritten**; the file is removed only once `switch direct` has
  finished. It and `docker/.env` are written through a temp file in the same
  directory and a rename.

### How a name's DNS changes: one batch, all or nothing

Every change of a hostname — its direct records out and the tunnel CNAME in,
or back — is **one** call to Cloudflare's
[batch endpoint](https://developers.cloudflare.com/dns/manage-dns-records/how-to/batch-record-changes/)
(`POST /zones/<id>/dns_records/batch` with `deletes` and `posts`), which
Cloudflare applies all or nothing. A failed call leaves the name exactly as
it was: never without a record, and with no window where the name answers
nothing (the old delete-then-create had one). A name with an A **and** an
AAAA moves in the same single call.

States the switch refuses to guess about, with nothing changed:

- a name with **no record and no wildcard** answering for it (it does not
  resolve today: it is neither "direct" nor something to snapshot);
- a name whose CNAME points at **another** `*.cfargotunnel.com` (neither
  direct nor ours);
- a name whose direct records **differ from its snapshot entry** (changed by
  hand while the snapshot existed): check which is right, fix the DNS or the
  entry.

### Names served by a wildcard

A zone often has no record per service, only a wildcard such as
`*.example.com A 203.0.113.10 proxied`. The switch handles it without ever
writing the wildcard:

- `switch tunnel` creates a proxied CNAME for a name that has no A/AAAA/CNAME
  of its own (an exact record wins over the wildcard) and records in the
  snapshot that the name had none, `via_wildcard: true`;
- `switch direct` deletes that CNAME and creates nothing: the name falls back
  to the wildcard. It never creates an A record for such a name, even with
  `CLOUDFLARE_DIRECT_ORIGIN` set or the snapshot lost;
- `*.<zone>` and any other wildcard record are never created, changed or
  deleted, and a wildcard hostname in the routes file is refused.

A wildcard covers one label: `*.example.com` answers for
`console.example.com`, not for `a.console.example.com`.

## Setup (once per environment)

1. **Create the tunnel** in [Zero Trust](https://one.dash.cloudflare.com) →
   Networks → Tunnels → Create a tunnel → Cloudflared. Name it after the stack
   (e.g. `orkestra-acme-production`). Copy the **token** — the long
   `eyJ…` string in the install command, not the 36-character tunnel id.
   **Do not add public hostnames** to it: a remote configuration overrides the
   local routes file, and the dashboard would also create DNS records itself.
   The routes stay in the repo only while the tunnel has none.
2. **One tunnel per environment.** Two hosts running one token are replicas
   of one tunnel, and Cloudflare spreads every hostname's requests over both —
   staging traffic would land on production. The switch refuses a token whose
   tunnel is not the `_tunnel_id` of this ENV's file, and names the
   environment when it belongs to another one.
3. **Routes file**: copy `docker/cloudflared/<ENV>.json.example` to
   `<ENV>.json`, set the hostnames, the services, `_smoke`, and `_tunnel_id`
   to the tunnel id (the switch prints it if you leave the placeholder). Keep
   it world-readable (`644`): cloudflared runs as a non-root uid. Commit and
   push it in the fork, then deploy (the deploy pulls the branch).
4. **API token**: My Profile → API Tokens → Create → custom, permission
   **Zone › DNS › Edit** on the zone of the hostnames, nothing else. No tunnel
   permission is needed: readiness is read from cloudflared itself.
5. **`docker/.env`** of the host:

   ```bash
   CLOUDFLARE_TUNNEL=false            # the switch flips it
   CLOUDFLARE_TUNNEL_TOKEN=eyJhIjoi...
   CLOUDFLARE_API_TOKEN=...
   CLOUDFLARE_ZONE_ID=...             # recommended: Overview page of the zone (else looked up from the hostnames' last two labels)
   CLOUDFLARE_DIRECT_ORIGIN=          # optional: see «Back to direct without a snapshot»
   CLOUDFLARE_DIRECT_CHECK_IP=        # optional: where `switch direct` checks the origin (see below)
   ```

   Set `CLOUDFLARE_ZONE_ID`: the lookup by name needs one more API call and
   guesses wrong for a zone like `example.co.uk`.

   The tunnel id is decoded from the token (base64 JSON `{a, t, s}`).
6. **Trusted proxies** — read «Client IP» below and fix `TRUSTED_PROXY_*`
   **before** switching.
7. `./orkestra.sh tunnel status` — cloudflared's state and, per hostname,
   whether its DNS is `direct` (with its records) or `tunnel`.

The host needs outbound access to the Cloudflare edge on port 7844 (UDP for
QUIC, else TCP for HTTP/2, which cloudflared falls back to), plus the
`cloudflare/cloudflared` and `curlimages/curl` images. Every staging or
production deploy pre-pulls both while `docker/cloudflared/<ENV>.json`
exists, so a switch mid-incident needs no registry; a failed pull is reported
as a pull failure (by the deploy as a warning, by the switch as the reason it
stops). Readiness uses the cloudflared image itself (`cloudflared tunnel
ready`), which is on the host as soon as cloudflared ran.

## Commands

Run from the stack's checkout (the one whose `docker/.env` is this ENV):

```bash
./orkestra.sh tunnel status
./orkestra.sh tunnel switch tunnel [--yes]   # direct → tunnel
./orkestra.sh tunnel switch direct [--yes]   # tunnel → direct
./orkestra.sh tunnel reload                  # after a change to docker/cloudflared/<ENV>.json
docker logs <APP_NAME>-cloudflared-<ENV>     # cloudflared's own log
```

Production asks for confirmation; `--yes` skips it (a non-interactive shell
needs it). Development is refused, and so is a shell `ENV=` that disagrees
with `docker/.env`.

The smoke test, in both directions: `https://<host><_smoke>?_orkestra_smoke=<n>`
(cache-busting query) through Cloudflare must answer **2xx or 3xx** — 000,
404 (the tunnel's catch-all: no route) and 5xx (502 origin, 522 timeout, 530
tunnel error) fail — in `TUNNEL_SMOKE_ROUNDS` (2) consecutive green rounds,
with retries per name for DNS propagation.

`switch tunnel`, in order — **the DNS moves only towards a tunnel proved to
work**:

1. refuses a token that is malformed, is the tunnel id, belongs to another
   environment or does not match `_tunnel_id`; refuses a same-name container
   of another compose project; classifies every name and refuses the states
   above;
2. `cloudflared tunnel ingress validate` on the routes file, in a throwaway
   container with no network; checks the external networks the stack
   declares (compose starts nothing while one is missing);
3. `up -d --no-deps cloudflared` with the `tunnel` profile, over the stack's
   files (infra + app, plus the observability and retrieval overlays while
   they run);
4. waits until `cloudflared tunnel ready` succeeds in cloudflared's network
   namespace (60 s at most). Not ready → stops, no DNS changed;
5. refuses if cloudflared's log shows a **remote configuration** (`Updated to
   new configuration`: the tunnel has public hostnames in the dashboard,
   which override the routes file);
6. probes every route from cloudflared's network namespace: each `service`
   at its `_smoke` path with the public `Host` header must answer 2xx/3xx
   (what the tunnel will do, proved before any name moves);
7. sets `CLOUDFLARE_TUNNEL=true` — before the first DNS write, so a deploy
   never drops cloudflared once a name may point at it;
8. per hostname not yet on the tunnel: writes its snapshot entry if it has
   none, then **one batch**: its A/AAAA/CNAME out, the proxied tunnel CNAME
   in. Other records of the name (TXT, MX…) are left alone. A failed batch
   leaves that name unchanged and stops the switch;
9. smoke test. A name that fails **and that this run moved** is **put back
   on its direct records automatically** (one batch from its snapshot entry);
   a name that was already on the tunnel before the run is never touched,
   and the others stay on the tunnel. If none is left on the tunnel, `CLOUDFLARE_TUNNEL` goes back to
   `false`. The command exits 1 and says where each name is.

`switch direct`, in order — **the DNS moves only towards an origin proved to
answer**:

1. decides, before any change, where every name goes: its snapshot entry
   (its records, or "no record of its own" when the wildcard answered — the
   wildcard must still exist), or for a name with no entry the rules of
   «Back to direct without a snapshot». It **reconciles every name whose
   current records differ from that** — on the tunnel, or left with no record
   by anything — not only the names on the tunnel;
2. checks the direct path of each name to move, Cloudflare bypassed: `curl -k
   --connect-to <host>:443:<target>:443 https://<host><_smoke>` must answer
   2xx/3xx, where the target is `CLOUDFLARE_DIRECT_CHECK_IP` if set, else the
   content of the record (or of the wildcard) the name goes back to. If the
   public IP does not hairpin from the host, set `CLOUDFLARE_DIRECT_CHECK_IP`
   to the reverse proxy's LAN address. A failure stops with nothing changed;
3. per name: **one batch**, the tunnel CNAME out and the snapshot records in
   (none for a wildcard name). A failed batch leaves the name on the tunnel;
   re-running reconciles it;
4. waits until cloudflared is **drained**: its `cloudflared_tunnel_total_requests`
   stays flat for 30 s (Cloudflare's edges follow the DNS within seconds to
   minutes), 3 minutes at most. With another connector of the tunnel running
   it warns that the drain is measured on this one only;
5. smoke test. A name that fails **and that this run moved** is **put back
   on the tunnel automatically** (a name already direct before the run is left
   alone);
   cloudflared, the snapshot and `CLOUDFLARE_TUNNEL=true` stay; exit 1;
6. only then removes cloudflared, sets `CLOUDFLARE_TUNNEL=false` and deletes
   the snapshot. If cloudflared never drained, it is left running with
   `CLOUDFLARE_TUNNEL=true`: run `switch direct` again later to remove it.

Both switches are idempotent and resumable: a name already where it should
be is skipped, a snapshot entry is never overwritten, and a failed run is
resumed by running it again. `reload` validates the routes file and probes
every route before restarting cloudflared, waits for it to be ready, and
refuses a remote configuration.

### Back to direct without a snapshot

The snapshot is per host and gitignored. For a hostname still on the tunnel
with no snapshot entry (snapshot lost, DNS moved by hand, a name created
directly on the tunnel), `switch direct` decides, before changing anything:

1. a wildcard answers for the name → only the tunnel CNAME is deleted;
2. else `CLOUDFLARE_DIRECT_ORIGIN` becomes a proxied record: `A` for an IPv4,
   `AAAA` for an IPv6, `CNAME` for a hostname;
3. else it refuses and changes nothing.

## Taking over from a hand-started connector

In an incident someone may start cloudflared by hand (`docker run … tunnel
run --token …`, any container name) and point the DNS at the tunnel by hand
before the stack's own service is configured. Two connectors of the **same**
tunnel are replicas, on the same host too, and Cloudflare spreads the
tunnel's requests over all of them — so check the hand-started one runs
**this environment's tunnel** (the `_tunnel_id` of
`docker/cloudflared/<ENV>.json`) and routes the same hostnames to the same
services. Then, in this order:

1. start the compose connector next to it:
   `cd docker && docker compose -p <APP_NAME>-<ENV> -f docker-compose.infra.yml -f docker-compose.prod.yml [overlays…] --env-file .env --profile tunnel up -d --no-deps cloudflared`
   (the stack's own project name and files; on staging
   `docker-compose.staging.yml` in place of `docker-compose.prod.yml`). The
   `-p` is required: without it Compose names the project after the
   directory (`docker`), and cloudflared lands on a network where the
   services do not resolve while Cloudflare already sends it requests. Check
   the name with `docker inspect -f '{{index .Config.Labels "com.docker.compose.project"}}' <APP_NAME>-backend-<ENV>`;
2. wait until it is ready:
   `docker run --rm --network container:<APP_NAME>-cloudflared-<ENV> cloudflare/cloudflared:2026.10.7 tunnel --metrics 127.0.0.1:20241 ready`
   exits 0 (retry for up to a minute);
3. remove the hand-started replica: `docker rm -f <its name>` (the switch
   never touches a container that is not `<APP_NAME>-cloudflared-<ENV>` of
   this compose project);
4. `./orkestra.sh tunnel switch tunnel --yes`: it finds cloudflared up, every
   name already on the tunnel (none is moved, no snapshot entry is written),
   sets `CLOUDFLARE_TUNNEL=true` and the smoke test is green;
5. `./orkestra.sh tunnel status`, and a smoke test in a browser.

While another connector of the tunnel runs, `status` and the switches name
it (any running container whose `TUNNEL_TOKEN`/`--token` carries this
tunnel's id), and only the HTTP answers are checked — a per-connector request
counter cannot tell which replica served a request. A name that was already on
the tunnel before a run is **never** rolled back by that run, whatever its
smoke test says: its way back (the wildcard, the old records) may be exactly
what is down.

Because the DNS was moved by hand, the snapshot has no entries for those
names: the way back is the wildcard or `CLOUDFLARE_DIRECT_ORIGIN` (above).
Set the latter if a name has neither.

The remote-configuration refusal does not apply to a connector started this
way with a local ingress file: on 2026-11-05 a production stack ran a
hand-started connector with a local file naming the compose services and no
public hostnames in the dashboard, which is the same shape as the compose
service, and was taken over in this order.

## Deploys, stop, reboot

- With `CLOUDFLARE_TUNNEL=true`, `./orkestra.sh deploy` decodes the token and
  checks its tunnel id against `_tunnel_id` (another environment's tunnel is
  refused), adds `--profile tunnel` to its compose commands (the infra
  `up` names its services, so it leaves cloudflared alone), runs `up -d
  --no-deps cloudflared` after the services and waits until it is ready: a
  redeploy never drops the only way in. Without the ENV's routes file or
  token it stops before building anything.
- Every staging/production deploy pre-pulls the cloudflared and curl images
  while the ENV has a routes file.
- The deploy leaves a running cloudflared alone unless its compose definition
  changed. **A change to the routes file needs `./orkestra.sh tunnel reload`**
  after the deploy that brought it.
- `./orkestra.sh stop` stops the app services and leaves cloudflared
  connected (it answers 502 until they are back); `stop --with-infra` (`down`)
  removes it too.
- `restart: unless-stopped` brings cloudflared back after a reboot.
- **Never set `CLOUDFLARE_TUNNEL=false` by hand while the DNS points at the
  tunnel**: the next `down` would remove cloudflared and the hostnames would
  answer 530. Use `switch direct`.

## Client IP (trusted proxies)

Through the tunnel the backend's peer is cloudflared, and `X-Forwarded-For`
holds only what the Cloudflare edge wrote — **one hop**. The direct path
through a reverse proxy usually has **two** (Cloudflare, then the proxy). A
hop count that does not match the mode collapses every caller onto one
address: one login rate-limit bucket for the whole internet, the operator IP
allowlist and the geo-block reading the wrong address, the wrong IP in every
audit event.

**Use `TRUSTED_PROXY_CIDRS`, not `TRUSTED_PROXY_COUNT`.** A hop count fits
one mode only (tunnel: 1, Cloudflare + reverse proxy: 2). CIDRs listing every
hop that is ours fit both, because the resolver skips trusted hops from the
right:

- the reverse proxy's address (the peer on the direct path);
- this stack's compose network (`docker network inspect
  <APP_NAME>-<ENV>_default`): the peer through the tunnel is cloudflared there;
- if the reverse proxy appends the Cloudflare edge to `X-Forwarded-For`,
  Cloudflare's published ranges as well
  (<https://www.cloudflare.com/ips-v4>, <https://www.cloudflare.com/ips-v6>).

Observed on a production stack, 2026-11-05: `TRUSTED_PROXY_CIDRS=192.168.88.2/32,172.20.0.0/24`
(HAProxy + the compose network) gave the right client IP in both modes;
before it the backend saw cloudflared's address for every caller.

`tunnel status` and `switch tunnel` warn when no CIDRs are set (with a hop
count of 1 too: it is wrong on the direct path) and when the CIDRs do not
cover this stack's compose network. Changing `TRUSTED_PROXY_*` takes a backend
recreate (`./orkestra.sh deploy --scope backend`). After the switch, check the
IP of a fresh login in the audit log.

## What the reverse proxy used to do: headers and CORS

In tunnel mode nothing sits between Cloudflare and the containers, so
anything the reverse proxy added is gone. Cloudflare zone rules (WAF, cache,
redirect, transform rules) still apply in both modes; what a proxy on the
origin did does not.

- **Security headers** come from the origin itself. The production console
  nginx (`docker/frontend-admin/nginx.conf`) sends `X-Frame-Options`,
  `X-Content-Type-Options`, `Referrer-Policy`, `Permissions-Policy` and
  `Strict-Transport-Security` on every document — restated in every
  `location` that adds a header of its own, because nginx drops inherited
  `add_header` there. The backend sets `nosniff`, `X-Frame-Options: DENY`,
  `Referrer-Policy` and HSTS (production-like).
  `docker/tests/frontend-nginx-cache.test.sh` pins the set. The console still
  ships no CSP (a known gap, independent of the mode). On staging the console
  and the client SPA are Vite dev servers, which add none of these: through
  the tunnel only the backend's headers and Cloudflare's own rules remain.
- **CORS preflight** is answered by the backend (`cmd/server/middleware.go`,
  per-audience allowlist, `Max-Age: 300`), never by the proxy. If the
  direct-path proxy answered `OPTIONS` itself, the tunnel answers with the
  backend's policy: a header the browser sends must be in `AllowedHeaders`.
- If the reverse proxy also adds a CSP or HSTS, both copies are sent in
  direct mode; a stricter proxy CSP wins there. Prefer removing such headers
  from the proxy once the origin sends them.

## What does not go through the tunnel

- **Hostnames not in the routes file** keep their direct path in both modes
  (untouched by the switch).
- **Non-HTTP traffic** — a tunnel carries HTTP(S) and WebSocket only (the
  backend's `/ws` and SSE `/stream` work). Mongo, Redis and the RustFS
  console are never public in either mode.
- **Outbound traffic** of the host (SMTP relays, OAuth providers, webhooks
  sent by the backend) never uses the tunnel.
- **Cloudflare's own limits** apply in both modes (100 MB request body on the
  Free/Pro plans, 100 s origin response timeout).

## Checklist — first switch of an environment

- [ ] Tunnel created in the dashboard, **no public hostnames**, token copied.
- [ ] `docker/cloudflared/<ENV>.json` committed with `_tunnel_id`, deployed;
      `ingress validate` OK.
- [ ] `CLOUDFLARE_TUNNEL_TOKEN`, `CLOUDFLARE_API_TOKEN` (DNS Edit only) in
      `docker/.env`.
- [ ] `TRUSTED_PROXY_CIDRS` covers the reverse proxy and the compose network,
      backend recreated; `CLOUDFLARE_ZONE_ID` set.
- [ ] `./orkestra.sh tunnel status` shows the tunnel id and every hostname
      `direct` (with its records, or `via *.<zone>`).
- [ ] `./orkestra.sh tunnel switch tunnel` → `Mode: tunnel`.
- [ ] In a browser: console login and refresh, an upload through the
      storage hostname if the routes have one, the API docs off; `curl -sI`
      on each hostname shows the security headers.
- [ ] Note the snapshot path (`docker/cloudflared/state/`) in the incident
      log; it is the way back.

## Troubleshooting

### «No routes file for <ENV>»

This repo ships only the examples. Create `docker/cloudflared/<ENV>.json` in
the fork, commit and deploy it. A deploy with `CLOUDFLARE_TUNNEL=true` and no
routes file refuses before building anything.

### «No _tunnel_id …» / «is tunnel X, but … pins Y» / «is the tunnel of staging»

The token belongs to another tunnel than the one pinned for this ENV. Use the
token of this environment's tunnel, or — if this is a new tunnel for this
ENV — write the id the message prints into `_tunnel_id`, commit and deploy.

### «cloudflared not ready after 60 s»

After `switch tunnel`: no DNS was changed. After `reload` or a deploy: the
DNS was not touched, and names already on the tunnel answer 530 until
cloudflared connects. `docker logs <APP_NAME>-cloudflared-<ENV>`:
`Unauthorized`/`invalid token` → wrong or revoked token; connection timeouts
→ the host has no egress to the edge on 7844 (check the firewall; cloudflared
falls back from QUIC/UDP to HTTP/2/TCP by itself); `config` errors → validate
the routes file.

### Smoke test: 404 on a hostname

The request reached cloudflared but matched no rule (the catch-all): the
hostname in the routes file differs from the DNS one, or the tunnel has a
dashboard (remote) configuration overriding the file — remove its public
hostnames. Logs show `Loading configuration from /etc/cloudflared/<ENV>.json`
when the local file is in use.

### Smoke test: 502

cloudflared cannot reach the service: wrong service name or port in the
routes file, or the service is down (`./orkestra.sh status`). Fix the file,
deploy it, `./orkestra.sh tunnel reload`.

### Smoke test: 530 / 1033

The hostname's CNAME points at a tunnel with no connected replica: cloudflared
is down or was removed while the DNS still pointed at it. `switch tunnel`
brings it back.

### Smoke test: 421 from the backend

The backend's host split refuses a `Host` it does not serve: `CONSOLE_HOST`
/ `CLIENT_API_HOST` must be the public hostnames. cloudflared keeps the
original `Host`, so this fails in both modes; it is a configuration error,
not a tunnel one.

### Mixed state after a failed switch

`status` shows some hostnames `tunnel` and some `direct` — after a failed
batch, or after a smoke test put the failing names back. Run `switch tunnel`
again (it resumes) or `switch direct` (it reconciles every name to its
snapshot entry). No batch can leave a name without its record.

### «The direct path does not answer for …»

`switch direct` checked the origin with Cloudflare bypassed and got no
2xx/3xx: the line or the reverse proxy is still down, or the host cannot
reach its own public IP (no hairpin NAT). Set `CLOUDFLARE_DIRECT_CHECK_IP` to
the reverse proxy's LAN address and retry. Nothing was changed.

### «Routes that do not answer from cloudflared»

The service of a route did not answer 2xx/3xx at its `_smoke` path from
cloudflared's network namespace: wrong service name or port, the service is
down, or the `_smoke` path needs authentication. Nothing was changed.

### «cloudflared received a REMOTE configuration»

The tunnel has public hostnames in the dashboard; their remote configuration
overrides the routes file. Delete them (Networks → Tunnels → the tunnel →
Public hostnames), then `./orkestra.sh tunnel reload`.

### «Another './orkestra.sh tunnel' is running»

Another tunnel command holds the stack's lock. Wait for it; the lock goes with
its process.

### «External network(s) missing»

Compose refuses to start any service, cloudflared included, while an external
network the stack declares (often in an overlay) is missing. Create it (`docker network create <name>`) and retry.

### «A container named … exists outside project …»

Something else owns the name `<APP_NAME>-cloudflared-<ENV>`. The switch never
touches it: find who started it (`docker inspect`, its compose labels) before
renaming or removing anything.
