// 20260903_oauth_provider_identity_unique.js — migration 0010
//
// Adds a unique index on (provider, providerId) to operator_oauth_providers
// and client_oauth_providers: one identity, one owner.
//
// It NEVER decides ownership. The existing (userUuid, provider) unique
// index already makes two rows of one user for one provider impossible, so
// every duplicate on (provider, providerId) is one identity claimed by two
// DIFFERENT users. Which of them owns it is a question about people, not
// about data: keeping the earliest-linked row would be an automatic answer
// to a question that has none, and the loser silently loses their ability
// to sign in.
//
// So: report every conflicting group, exit non-zero (quit(1) — under the
// stdin run below mongosh swallows a thrown error and exits 0, which was
// measured, not assumed), change nothing. Reconciliation is explicit — prepend a RESOLVE map naming, per
// identity, the userUuid that KEEPS it:
//
//   set -a; . docker/.env; set +a
//   { echo 'var RESOLVE = {"google:1234": "<userUuid>"};'; \
//     cat backend/migrations/20260903_oauth_provider_identity_unique.js; } | \
//   docker exec -i "${APP_NAME}-mongodb-${ENV}" mongosh --quiet \
//     -u "$MONGO_ROOT_USERNAME" -p "$MONGO_ROOT_PASSWORD" \
//     --authenticationDatabase admin "$MONGO_DATABASE"
//
// Only rows of listed identities are deleted, only the losers, and every
// deletion is printed so the output can be filed with the change. Unlisted
// groups still block. With zero groups left the index is created on both
// collections and verified; a re-run is a no-op.
//
// Run this BEFORE deploying the release that ships ownership-first OAuth
// writes: that release verifies the index at boot and degrades OAuth to
// oauth_store_unavailable when it is missing, rather than running the
// ownership flow without the constraint it relies on. Runbook:
// docs/migrations/0010_oauth_provider_identity_unique.md
//
// Plain run (no conflicts expected):
//
//   set -a; . docker/.env; set +a
//   docker exec -i "${APP_NAME}-mongodb-${ENV}" mongosh --quiet \
//     -u "$MONGO_ROOT_USERNAME" -p "$MONGO_ROOT_PASSWORD" \
//     --authenticationDatabase admin "$MONGO_DATABASE" \
//     < backend/migrations/20260903_oauth_provider_identity_unique.js

const COLLECTIONS = ["operator_oauth_providers", "client_oauth_providers"];
const INDEX_NAME = "provider_1_providerId_1";
const KEYS = { provider: 1, providerId: 1 };
const OPTS = { name: INDEX_NAME, unique: true };
const resolve = typeof RESOLVE === "undefined" ? {} : RESOLVE;

// Everything runs inside one try/catch that ends with quit(1): piped on
// stdin, mongosh prints an uncaught error, keeps going and exits 0, which
// would report a half-run migration as a success.
try {

let blocked = 0;

for (const name of COLLECTIONS) {
  if (!db.getCollectionNames().includes(name)) {
    print(`${name}: collection absent — nothing to check`);
    continue;
  }
  const coll = db.getCollection(name);
  const groups = coll
    .aggregate(
      [
        {
          $group: {
            _id: { provider: "$provider", providerId: "$providerId" },
            rows: { $push: "$$ROOT" },
            n: { $sum: 1 },
          },
        },
        { $match: { n: { $gt: 1 } } },
      ],
      { allowDiskUse: true },
    )
    .toArray();

  for (const g of groups) {
    const identity = `${g._id.provider}:${g._id.providerId}`;
    const keeper = resolve[identity];

    if (!keeper) {
      blocked++;
      print(`CONFLICT ${name} ${identity} — claimed by ${g.n} users:`);
      for (const r of g.rows) {
        print(
          `  userUuid=${r.userUuid} linkedAt=${r.linkedAt} lastUsed=${r.lastUsed} tokenStatus=${r.tokenStatus}`,
        );
      }
      continue;
    }

    const owns = g.rows.some((r) => r.userUuid === keeper);
    if (!owns) {
      blocked++;
      print(
        `CONFLICT ${name} ${identity} — RESOLVE names ${keeper}, which does not hold this identity. Nothing deleted.`,
      );
      continue;
    }

    for (const r of g.rows) {
      if (r.userUuid === keeper) continue;
      coll.deleteOne({ _id: r._id });
      print(`deleted ${name} uuid=${r.uuid} userUuid=${r.userUuid} identity=${identity}`);
    }
  }
}

if (blocked > 0) {
  print("");
  print(
    `${blocked} identity conflict(s) unresolved. Re-run with a RESOLVE map naming the keeper per identity.`,
  );
  print("No index was created. See docs/migrations/0010_oauth_provider_identity_unique.md");
  quit(1);
}

for (const name of COLLECTIONS) {
  const coll = db.getCollection(name);
  // Idempotent — same name + options is a no-op on a migrated database;
  // createIndex also creates a collection that does not exist yet (and
  // getIndexes() on an absent collection throws, so it is not consulted
  // until the collection exists).
  const exists = db.getCollectionNames().includes(name);
  if (!exists || !coll.getIndexes().find((i) => i.name === INDEX_NAME)) {
    coll.createIndex(KEYS, OPTS);
  }
  const now = coll.getIndexes().find((i) => i.name === INDEX_NAME);
  if (!now || !now.unique) {
    print(`FAILED ${name}.${INDEX_NAME} missing or non-unique after createIndex`);
    quit(1);
  }
  print(`ok ${name} ${INDEX_NAME} ${JSON.stringify(now.key)} unique`);
}
print("migration 0010 complete");

} catch (e) {
  print(`FAILED migration 0010: ${e && e.message ? e.message : e}`);
  quit(1);
}
