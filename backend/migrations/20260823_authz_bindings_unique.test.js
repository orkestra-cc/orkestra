// Self-verifying companion to 20260823_authz_bindings_unique.js.
//
// It seeds duplicate-binding fixtures, runs the REAL migration file via
// load() (never a copy of its pipeline — a re-implementation here would
// drift silently and prove nothing), and asserts the property that actually
// matters: **deduplication must not change effective access at any instant**.
//
// That invariant is what the original "keep the earliest grantedAt" rule
// broke. A tuple holding an expiring grant plus a later permanent one would
// keep the expiring row and discard the permanent one, so the user lost the
// role the moment the survivor expired — and lost it immediately when the
// older row had already expired. Fixture C is exactly that case and fails
// loudly against the pre-fix migration.
//
// Runs against a THROWAWAY database: it drops authz_bindings on entry and
// on exit. Never point it at a real one.
//
// Run (from the repo root, with mongosh on PATH):
//
//   mongosh --quiet "mongodb://localhost:27017/authz_migration_test" \
//     backend/migrations/20260823_authz_bindings_unique.test.js
//
// Run against the stack's containerized mongod (no host mongosh needed):
//
//   set -a; . docker/.env; set +a
//   C="${APP_NAME}-mongodb-${ENV}"
//   docker cp backend/migrations/20260823_authz_bindings_unique.js      "$C":/tmp/
//   docker cp backend/migrations/20260823_authz_bindings_unique.test.js "$C":/tmp/
//   docker exec "$C" mongosh --quiet \
//     -u "$MONGO_ROOT_USERNAME" -p "$MONGO_ROOT_PASSWORD" \
//     --authenticationDatabase admin "authz_migration_test" \
//     --eval 'MIGRATION_JS="/tmp/20260823_authz_bindings_unique.js"' \
//     --file /tmp/20260823_authz_bindings_unique.test.js

// Path to the migration under test. Override with --eval before --file when
// the two files are not side by side in the working directory.
const MIGRATION =
  typeof MIGRATION_JS !== "undefined"
    ? MIGRATION_JS
    : "backend/migrations/20260823_authz_bindings_unique.js";

const COLLECTION = "authz_bindings";
const HOUR = 60 * 60 * 1000;
const now = new Date();
const at = (ms) => new Date(now.getTime() + ms);

let failures = 0;
function check(ok, what) {
  if (ok) {
    print(`  ok   ${what}`);
  } else {
    failures++;
    print(`  FAIL ${what}`);
  }
}

// --- fixtures ----------------------------------------------------------
//
// One tuple per scenario, all seeded together so the migration runs once.
// `keep` names the uuid that MUST survive.

const fixtures = [
  {
    tuple: "A-expiring-then-permanent",
    keep: "A2",
    rows: [
      { uuid: "A1", grantedAt: at(-48 * HOUR), expiresAt: at(6 * HOUR) },
      { uuid: "A2", grantedAt: at(-24 * HOUR), expiresAt: null },
    ],
  },
  {
    tuple: "B-permanent-then-expiring",
    keep: "B1",
    rows: [
      { uuid: "B1", grantedAt: at(-48 * HOUR), expiresAt: null },
      { uuid: "B2", grantedAt: at(-24 * HOUR), expiresAt: at(6 * HOUR) },
    ],
  },
  {
    // The severe one: the oldest row has ALREADY expired. "Keep the
    // earliest grantedAt" discards the live permanent grant and revokes the
    // role the instant the migration runs.
    tuple: "C-expired-then-permanent",
    keep: "C2",
    rows: [
      { uuid: "C1", grantedAt: at(-48 * HOUR), expiresAt: at(-6 * HOUR) },
      { uuid: "C2", grantedAt: at(-24 * HOUR), expiresAt: null },
    ],
  },
  {
    // Deliberately ordered so oldest != furthest-future: the older row
    // expires FIRST, so "keep the earliest grantedAt" shortens the grant.
    tuple: "D-two-expiring",
    keep: "D2",
    rows: [
      { uuid: "D1", grantedAt: at(-48 * HOUR), expiresAt: at(6 * HOUR) },
      { uuid: "D2", grantedAt: at(-24 * HOUR), expiresAt: at(72 * HOUR) },
    ],
  },
  {
    // No expiry anywhere: the historical tie-break (earliest grantedAt)
    // must be preserved exactly.
    tuple: "E-two-permanent",
    keep: "E1",
    rows: [
      { uuid: "E1", grantedAt: at(-48 * HOUR), expiresAt: null },
      { uuid: "E2", grantedAt: at(-24 * HOUR), expiresAt: null },
    ],
  },
  {
    // A global/system-role grant (tenantId "") — the index covers these too.
    tuple: "F-global-expired-then-permanent",
    keep: "F2",
    global: true,
    rows: [
      { uuid: "F1", grantedAt: at(-48 * HOUR), expiresAt: at(-6 * HOUR) },
      { uuid: "F2", grantedAt: at(-24 * HOUR), expiresAt: null },
    ],
  },
  {
    tuple: "G-single-row",
    keep: "G1",
    rows: [{ uuid: "G1", grantedAt: at(-48 * HOUR), expiresAt: at(6 * HOUR) }],
  },
];

const coll = db.getCollection(COLLECTION);
coll.drop();

const docs = [];
for (const f of fixtures) {
  for (const r of f.rows) {
    docs.push({
      uuid: r.uuid,
      userUUID: "user-" + f.tuple,
      tenantId: f.global ? "" : "tenant-" + f.tuple,
      roleId: "role-" + f.tuple,
      roleName: "org_owner",
      grantedBy: "system",
      grantedAt: r.grantedAt,
      expiresAt: r.expiresAt,
    });
  }
}
coll.insertMany(docs);

// --- effective access --------------------------------------------------
//
// Mirrors repository.ListActiveBindingsForUser's filter exactly: a grant is
// effective when expiresAt is null/missing, or strictly in the future.
// {expiresAt: null} matches a missing field too, which is why a row must
// never be considered expired on the strength of a null.
function effectiveGrants(instant) {
  const held = coll
    .find({ $or: [{ expiresAt: null }, { expiresAt: { $gt: instant } }] })
    .toArray()
    .map((b) => `${b.tenantId}|${b.userUUID}|${b.roleId}`);
  return [...new Set(held)].sort();
}

// Probe instants chosen to straddle every fixture expiry boundary.
const probes = [
  ["now-2h", at(-2 * HOUR)],
  ["now", now],
  ["now+3h", at(3 * HOUR)],
  ["now+12h", at(12 * HOUR)],
  ["now+100h", at(100 * HOUR)],
  ["now+10y", at(10 * 365 * 24 * HOUR)],
];

const before = {};
for (const [label, instant] of probes) before[label] = effectiveGrants(instant);

const totalBefore = coll.countDocuments({});

// --- run the real migration -------------------------------------------

print(`\nseeded ${totalBefore} binding row(s) across ${fixtures.length} tuple(s)`);
print(`running migration: ${MIGRATION}\n`);
load(MIGRATION);

// --- assertions --------------------------------------------------------

print("\n--- effective access is unchanged at every probed instant ---");
for (const [label, instant] of probes) {
  const after = effectiveGrants(instant);
  const b = before[label];
  const lost = b.filter((g) => !after.includes(g));
  const gained = after.filter((g) => !b.includes(g));
  check(
    lost.length === 0 && gained.length === 0,
    `@${label}: ${b.length} grant(s) before, ${after.length} after` +
      (lost.length ? ` — REVOKED BY DEDUP: ${lost.join(", ")}` : "") +
      (gained.length ? ` — spuriously gained: ${gained.join(", ")}` : ""),
  );
}

print("\n--- the surviving row is the one conferring the most access ---");
for (const f of fixtures) {
  const rows = coll
    .find({ roleId: "role-" + f.tuple })
    .toArray()
    .map((r) => r.uuid);
  check(
    rows.length === 1 && rows[0] === f.keep,
    `${f.tuple}: survivor ${JSON.stringify(rows)}, want ["${f.keep}"]`,
  );
}

print("\n--- the unique index exists and is unique ---");
const idx = coll.getIndexes().find((i) => i.name === "tenantId_1_userUUID_1_roleId_1");
check(!!idx && idx.unique === true, `index present and unique: ${JSON.stringify(idx)}`);

print("\n--- rerunning the migration is a no-op ---");
const afterFirst = coll.countDocuments({});
load(MIGRATION);
check(
  coll.countDocuments({}) === afterFirst,
  `row count stable across a second run: ${afterFirst}`,
);

coll.drop();

print("");
if (failures > 0) {
  throw new Error(`${failures} assertion(s) FAILED`);
}
print(`all assertions passed (${totalBefore} seeded rows, ${fixtures.length} tuples)`);
