# tests/backup/

Regression coverage for `backup.sh` / `scripts/backup-cron.sh`. These are
plain bash scripts, not part of any Go/Node/Flutter test runner — run them
directly (`./tests/backup/test_foo.sh`) or loop over the directory.

## Do not add this directory to `make ci` / CI workflows

`docker/.env` is gitignored and is never present in a CI runner. Five of the
seven tests here need it (they source it to build a corrupted copy — wrong
password, wrong secret key, wrong stack name — and point `backup.sh` at a
live stack via `ORKESTRA_ENV_FILE`). The two stubbed ones
(`test_rustfs_empty_install.sh`, `test_redis_copy_failure.sh`) fall back to
`docker/.env.example`, so those two do run on a fresh checkout. Wiring this directory into `make ci`
as-is would produce a suite that is permanently green because every one of
those five prints `SKIP` and exits 0 without ever having exercised the code
it claims to cover — exactly the false-confidence failure mode this backup
remediation branch exists to close elsewhere. If CI coverage for these is
ever wanted, it needs a real stack (mongodb/redis/rustfs containers +
docker/.env) provisioned in the runner, not just `docker/.env.example`
copied into place.

Outside CI, these tests drive the real local stack: a genuine blocking
Redis `SAVE`, a real (rejected) `aws s3 sync` against rustfs, a real
`mongodump` against a corrupted password. They are safe to run against a
disposable/dev stack but are not free — do not run them casually against a
stack you care about being fast or quiet at that moment.

## Running them

Five of the seven (everything except `test_rustfs_empty_install.sh` and
`test_redis_copy_failure.sh`, which stub `docker` entirely) require an
explicit opt-in on top of `docker/.env` being present, so a bare
`./tests/backup/test_x.sh` never silently touches live infrastructure:

```bash
ORKESTRA_LIVE_STACK_TESTS=1 ./tests/backup/test_all_requires_components.sh
ORKESTRA_LIVE_STACK_TESTS=1 ./tests/backup/test_capture_failure_required.sh
ORKESTRA_LIVE_STACK_TESTS=1 ./tests/backup/test_redis_capture_failure.sh
ORKESTRA_LIVE_STACK_TESTS=1 ./tests/backup/test_rustfs_capture_failure.sh
ORKESTRA_LIVE_STACK_TESTS=1 ./tests/backup/test_secrets_capture_failure.sh
```

Without `ORKESTRA_LIVE_STACK_TESTS=1` each of these prints
`SKIP: set ORKESTRA_LIVE_STACK_TESTS=1 ...` and exits 0 — treat that as "not
run", not as a pass.

Two are the exception: they are genuinely hermetic. Each stubs `docker` on
`PATH` with a fake that answers only the calls its scenario expects and exits
loudly (rc=9, `FAKE DOCKER: unexpected ...`) on anything else, so the test
cannot quietly take a different path and still pass. They touch no live
container and no network. Run them any time, no opt-in needed:

```bash
./tests/backup/test_rustfs_empty_install.sh   # bucket-less install is a legitimate empty capture
./tests/backup/test_redis_copy_failure.sh     # a failed `docker cp` fails the redis component
```

- **`test_rustfs_empty_install.sh`** — the fake answers a container-name `ps`
  check and a `list-buckets` call returning nothing, and rejects a sync
  attempt, since the whole point is that one must never happen.
- **`test_redis_copy_failure.sh`** — the fake is a healthy Redis (SAVE returns
  `OK`, both the RDB and the AOF directory exist) whose RDB `docker cp` fails
  while the AOF copy succeeds. That pairing is the regression: before the copies
  were checked, the surviving AOF satisfied the end-of-function existence check
  on the failed RDB's behalf, and the component was reported captured.

They read the *shape* of an env file to build their own fake one, preferring
`docker/.env` and falling back to the tracked `docker/.env.example`, so they
also run on a fresh checkout. Nothing in it is trusted or required to be
valid — every docker call is stubbed.
