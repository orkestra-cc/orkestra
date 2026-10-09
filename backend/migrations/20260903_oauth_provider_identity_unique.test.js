// Self-verifying companion to 20260903_oauth_provider_identity_unique.js.
//
// Migration 0010 adds a unique (provider, providerId) index to both OAuth
// provider collections. It NEVER decides ownership: the existing
// (userUuid, provider) unique index already makes two rows of one user for
// one provider impossible, so every duplicate on (provider, providerId) is
// one identity claimed by two DIFFERENT users — a conflict no rule can
// settle. These tests pin that contract: a conflict blocks and changes
// nothing, a RESOLVE entry deletes only the named identity's losers and
// prints every deletion, a bad keeper is refused, a clean database gets the
// index on BOTH collections, and a re-run is a no-op.
//
// Each scenario runs the REAL migration file in its own mongosh process,
// exactly as production does (the file piped on stdin), with the RESOLVE
// map prepended to the script. Runs against a THROWAWAY database that is
// dropped on exit. Never point it at a real one.
//
// Run (host mongosh on PATH, unauthenticated local mongod):
//
//   MONGO_URI='mongodb://127.0.0.1:27017' \
//     node --test backend/migrations/20260903_oauth_provider_identity_unique.test.js
//
// Run against the stack's containerized mongod (no host mongosh needed):
//
//   set -a; . docker/.env; set +a
//   MONGOSH="docker exec -i ${APP_NAME}-mongodb-${ENV} mongosh \
//       -u $MONGO_ROOT_USERNAME -p $MONGO_ROOT_PASSWORD --authenticationDatabase admin" \
//     node --test backend/migrations/20260903_oauth_provider_identity_unique.test.js
//
// MONGOSH is the shell command that reaches mongosh (default: `mongosh`,
// with MONGO_URI as its connection string when set); MIGRATION_TEST_DB
// names the throwaway database (default: oauth_migration_test).

'use strict'

const { test, after } = require('node:test')
const assert = require('node:assert/strict')
const { spawnSync } = require('node:child_process')
const fs = require('node:fs')
const path = require('node:path')

const MIGRATION = path.join(__dirname, '20260903_oauth_provider_identity_unique.js')
const TEST_DB = process.env.MIGRATION_TEST_DB || 'oauth_migration_test'
const COLLECTIONS = ['operator_oauth_providers', 'client_oauth_providers']
const INDEX_NAME = 'provider_1_providerId_1'

const BASE = process.env.MONGOSH || `mongosh${process.env.MONGO_URI ? ` '${process.env.MONGO_URI}'` : ''}`

// mongosh runs `script` against TEST_DB in a fresh process and returns the
// exit code and output. The script travels on stdin, as the production run
// recipe sends the migration — which is also why the migration ends a
// conflict with quit(1): piped on stdin, mongosh swallows a thrown error
// and exits 0.
function mongosh(script) {
  const res = spawnSync('bash', ['-c', `${BASE} --quiet ${TEST_DB}`], { input: script, encoding: 'utf8' })
  if (res.error) throw res.error
  return { exitCode: res.status, stdout: res.stdout || '', stderr: res.stderr || '' }
}

// evalLast runs one expression with --eval, whose output is the bare value
// (stdin mode echoes a prompt on every line), and returns its last line.
function evalLast(expr) {
  const res = spawnSync('bash', ['-c', `${BASE} --quiet ${TEST_DB} --eval ${JSON.stringify(expr)}`], { encoding: 'utf8' })
  if (res.error) throw res.error
  assert.equal(res.status, 0, `eval failed: ${res.stdout}\n${res.stderr}`)
  return res.stdout.trim().split('\n').pop()
}

function seed(fixtures) {
  const lines = COLLECTIONS.map((c) => `db.getCollection(${JSON.stringify(c)}).drop();`)
  for (const [coll, docs] of Object.entries(fixtures)) {
    if (docs.length > 0) {
      lines.push(`db.getCollection(${JSON.stringify(coll)}).insertMany(${JSON.stringify(docs)});`)
    }
  }
  const res = mongosh(lines.join('\n'))
  assert.equal(res.exitCode, 0, `seed failed: ${res.stdout}\n${res.stderr}`)
}

function runMigration(opts = {}) {
  if (!fs.existsSync(MIGRATION)) {
    return { exitCode: 2, stdout: '', stderr: `migration file missing: ${MIGRATION}` }
  }
  const resolve = opts.RESOLVE ? `var RESOLVE = ${JSON.stringify(opts.RESOLVE)};\n` : ''
  return mongosh(resolve + fs.readFileSync(MIGRATION, 'utf8'))
}

function count(coll, filter = {}) {
  return Number(evalLast(`db.getCollection(${JSON.stringify(coll)}).countDocuments(${JSON.stringify(filter)})`))
}

function hasUniqueIndex(coll) {
  // getIndexes() throws on a collection that does not exist: that is "no index".
  return (
    evalLast(
      `db.getCollectionNames().includes(${JSON.stringify(coll)}) && db.getCollection(${JSON.stringify(coll)}).getIndexes().some((i) => i.name === ${JSON.stringify(INDEX_NAME)} && i.unique === true)`,
    ) === 'true'
  )
}

after(() => {
  mongosh('db.dropDatabase();')
})

test('a cross-user duplicate group blocks the migration and changes nothing', () => {
  seed({
    operator_oauth_providers: [
      { uuid: 'a', userUuid: 'u-1', provider: 'google', providerId: '1234', linkedAt: new Date('2026-01-01') },
      { uuid: 'b', userUuid: 'u-2', provider: 'google', providerId: '1234', linkedAt: new Date('2026-02-01') },
    ],
  })

  const res = runMigration()

  assert.notEqual(res.exitCode, 0, 'a conflict must exit non-zero')
  const out = res.stdout + res.stderr
  assert.match(out, /google:1234/, 'the group must be printed')
  assert.match(out, /u-1/)
  assert.match(out, /u-2/)
  assert.equal(count('operator_oauth_providers'), 2, 'nothing may be deleted')
  assert.equal(hasUniqueIndex('operator_oauth_providers'), false, 'no index while a conflict stands')
})

test('a RESOLVE entry deletes only the losing rows of that identity', () => {
  seed({
    operator_oauth_providers: [
      { uuid: 'a', userUuid: 'u-1', provider: 'google', providerId: '1234' },
      { uuid: 'b', userUuid: 'u-2', provider: 'google', providerId: '1234' },
      { uuid: 'c', userUuid: 'u-3', provider: 'github', providerId: '9999' },
      { uuid: 'd', userUuid: 'u-4', provider: 'github', providerId: '9999' },
    ],
  })

  const res = runMigration({ RESOLVE: { 'google:1234': 'u-1' } })

  assert.notEqual(res.exitCode, 0, 'the unresolved github group must still block')
  assert.equal(count('operator_oauth_providers', { uuid: 'b' }), 0, 'the named loser is deleted')
  assert.equal(count('operator_oauth_providers', { uuid: 'a' }), 1, 'the keeper survives')
  assert.equal(count('operator_oauth_providers', { provider: 'github' }), 2, 'an unresolved group is untouched')
  assert.match(res.stdout, /deleted .*uuid=b\b/, 'every deletion must be printed for the change record')
})

test('a RESOLVE entry naming a userUuid that does not own the identity is refused', () => {
  seed({
    operator_oauth_providers: [
      { uuid: 'a', userUuid: 'u-1', provider: 'google', providerId: '1234' },
      { uuid: 'b', userUuid: 'u-2', provider: 'google', providerId: '1234' },
    ],
  })
  const res = runMigration({ RESOLVE: { 'google:1234': 'u-999' } })
  assert.notEqual(res.exitCode, 0)
  assert.equal(count('operator_oauth_providers'), 2, 'nothing deleted on a bad keeper')
})

test('no conflicts: the index is created and verified on BOTH collections', () => {
  seed({
    operator_oauth_providers: [{ uuid: 'a', userUuid: 'u-1', provider: 'google', providerId: '1234' }],
    client_oauth_providers: [{ uuid: 'z', userUuid: 'c-1', provider: 'google', providerId: '5555' }],
  })
  const res = runMigration()
  assert.equal(res.exitCode, 0, `${res.stdout}\n${res.stderr}`)
  assert.equal(hasUniqueIndex('operator_oauth_providers'), true)
  assert.equal(hasUniqueIndex('client_oauth_providers'), true)
})

test('a re-run on a migrated database is a no-op', () => {
  seed({ operator_oauth_providers: [{ uuid: 'a', userUuid: 'u-1', provider: 'google', providerId: '1234' }] })
  assert.equal(runMigration().exitCode, 0)
  const second = runMigration()
  assert.equal(second.exitCode, 0, `${second.stdout}\n${second.stderr}`)
  assert.equal(count('operator_oauth_providers'), 1)
  assert.equal(hasUniqueIndex('operator_oauth_providers'), true)
})

test('an empty database migrates cleanly and creates the index on both collections', () => {
  seed({})
  const res = runMigration()
  assert.equal(res.exitCode, 0, `${res.stdout}\n${res.stderr}`)
  assert.equal(hasUniqueIndex('operator_oauth_providers'), true)
  assert.equal(hasUniqueIndex('client_oauth_providers'), true)
})
