# tests/backup/

Regression coverage for `backup.sh` / `scripts/backup-cron.sh`. These are
plain bash scripts, not part of any Go/Node/Flutter test runner — run them
directly (`./tests/backup/test_foo.sh`) or loop over the directory.

## Do not add this directory to `make ci` / CI workflows

`docker/.env` is gitignored and is never present in a CI runner. Five of the
six tests here need it (they source it to build a corrupted copy — wrong
password, wrong secret key, wrong stack name — and point `backup.sh` at a
live stack via `ORKESTRA_ENV_FILE`). Wiring this directory into `make ci`
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

Five of the six (everything except `test_rustfs_empty_install.sh`) require
an explicit opt-in on top of `docker/.env` being present, so a bare
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

`test_rustfs_empty_install.sh` is the one exception: it is genuinely
hermetic. It stubs `docker` on `PATH` with a fake that only answers the two
calls a bucket-less rustfs capture is expected to make (a container-name
`ps` check and a `list-buckets` call returning nothing) and exits loudly
(rc=9, `FAKE DOCKER: unexpected ...`) on any other invocation — a sync
attempt in particular, since the whole point of the test is that one must
never happen. It touches no live container, no real docker/.env values, and
no network. Run it any time, no opt-in needed:

```bash
./tests/backup/test_rustfs_empty_install.sh
```

(It still reads the *shape* of `docker/.env` — via `sed` on top of the real
file — to build its own fake env, so it needs `docker/.env` to exist on
disk, but nothing in it is trusted or required to be valid.)
