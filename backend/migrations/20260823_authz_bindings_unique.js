// 0009 — Add the first uniqueness constraint on authz_bindings:
// (tenantId, userUUID, roleId), and dedup any rows that already violate it.
//
// authz_bindings has never carried a unique index — not even on uuid.
// Repository.CreateBinding is a plain InsertOne with no upsert and no
// duplicate handling, so replaying a tenant-provisioning step (a lost
// response, a crashed executor, an expired lease — see the setup-saga
// work this migration is a prerequisite for) silently accumulates
// duplicate org_owner / org_admin / custom-role grants for the same
// (tenant, user, role) tuple. GetEffectivePermissions unions every
// binding, so duplicates are otherwise invisible until someone counts
// rows — but every duplicate is a wasted document forever and a sign
// the grant path is not safe to retry.
//
// tenantId=="" rows are global/system-role grants (super_admin,
// administrator, developer, manager, operator, guest). The index
// covers them too, deliberately: a user must not hold the same system
// role twice globally any more than they should hold the same tenant
// role twice in one tenant. NOT sparse, NO partial filter — empty
// string is an ordinary indexed value here, not a value to skip.
//
// Declared spec: backend/internal/core/authz/module.go (Collections(),
// CollBindings block). module.go:ensureCollections is create-only AND
// deliberately non-fatal on failure (it logs a WARN and continues) —
// so on an installation with pre-existing duplicates the unique index
// silently fails to build at boot and the constraint simply does not
// exist, while every health check stays green. Nothing else will ever
// tell you the index is missing. RUN THIS MIGRATION BEFORE the deploy
// that ships the CollBindings index change (and, per the setup-saga
// design doc, before the PR 5 deploy that enables the finalize route —
// that route replays provisioning steps and depends on this index
// existing to make the owner-binding grant safely idempotent).
//
// Idempotent: rerunning after a successful pass finds zero duplicate
// groups (the dedup aggregation only matches tuples with >1 row) and
// finds the index already present under its name, so createIndex is
// skipped. Safe to run twice, and safe to run before this index spec
// has shipped in Go (the script only touches authz_bindings).
//
// Run:
//   set -a; . docker/.env; set +a
//   docker exec -i "${APP_NAME}-mongodb-${ENV}" mongosh --quiet \
//     -u "$MONGO_ROOT_USERNAME" -p "$MONGO_ROOT_PASSWORD" \
//     --authenticationDatabase admin "$MONGO_DATABASE" \
//     < backend/migrations/20260823_authz_bindings_unique.js

const COLL = "authz_bindings";
const NAME = "tenantId_1_userUUID_1_roleId_1";
const KEYS = { tenantId: 1, userUUID: 1, roleId: 1 };
const OPTS = { name: NAME, unique: true };

const c = db.getCollection(COLL);
if (!db.getCollectionNames().includes(COLL)) {
  print(`${COLL}: collection absent — nothing to do`);
} else {
  // 1. Dedup: per (tenantId, userUUID, roleId) keep the grant that confers
  //    the MOST access. Deduplicating must never revoke a privilege, and
  //    "oldest wins" does exactly that whenever a tuple holds both an
  //    expiring grant and a later permanent one — the common shape when a
  //    trial/contractor grant was later made permanent. Keeping the older
  //    row there discards the permanent grant and the user loses the role
  //    the moment the survivor expires; if the older row has ALREADY
  //    expired, access is lost the instant this migration runs.
  //
  //    Ranking: permanent (expiresAt null or missing) beats every expiring
  //    grant; among expiring grants the furthest-future expiry wins; ties
  //    fall back to the earliest grantedAt and then _id, so the winner is
  //    total and reproducible across replicas and reruns.
  //
  //    _perm cannot be folded into the sort on expiresAt: BSON's canonical
  //    type ordering sorts null BELOW dates, so `expiresAt: -1` on its own
  //    would rank a grant expiring tomorrow ABOVE a permanent one.
  //
  //    Expired rows are NOT reaped here — that is a separate concern (this
  //    collection has no TTL and no reaper; see authz/CLAUDE.md). The
  //    runtime grant paths reap the tuple's own expired row when a role is
  //    granted again, so an expired survivor never becomes un-re-grantable.
  const dups = c.aggregate([
    { $addFields: { _perm: { $cond: [{ $eq: [{ $type: "$expiresAt" }, "date"] }, 0, 1] } } },
    { $sort: { _perm: -1, expiresAt: -1, grantedAt: 1, _id: 1 } },
    { $group: { _id: { t: "$tenantId", u: "$userUUID", r: "$roleId" },
                keep: { $first: "$_id" }, all: { $push: "$_id" } } },
    { $match: { $expr: { $gt: [{ $size: "$all" }, 1] } } },
  ], { allowDiskUse: true }).toArray();
  let removed = 0;
  for (const d of dups) {
    const losers = d.all.filter((id) => !id.equals(d.keep));
    removed += c.deleteMany({ _id: { $in: losers } }).deletedCount;
  }
  print(`${COLL}: removed ${removed} duplicate binding row(s) across ${dups.length} tuple(s)`);
  // 2. Unique index (idempotent — same name+options is a no-op).
  if (!c.getIndexes().find((i) => i.name === NAME)) {
    c.createIndex(KEYS, OPTS);
  }
  const now = c.getIndexes().find((i) => i.name === NAME);
  print(`  -> ${JSON.stringify(now)}`);
  if (!now || !now.unique) {
    throw new Error(`${COLL}.${NAME} missing or non-unique after createIndex`);
  }
  print("reconcile complete");
}
