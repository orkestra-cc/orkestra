#!/usr/bin/env bash
#
# Nightly backup wrapper. Non-interactive, fails loudly, prunes by age.
# Scheduled from /etc/cron.d/orkestra-<stack>-backup (see docs/site/operating/backup-and-restore.mdx).
#
# backup.sh already refuses to write a partial backup (its --require gate exits 3
# when a component is missing or fails to capture). The verification below is
# deliberate defence in depth: it re-checks the produced artifact rather than
# trusting an exit code, so a future regression in backup.sh cannot turn this
# schedule back into the silent no-op it was before 2026-08-07.
#
set -Eeuo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

RETENTION_DAYS="${BACKUP_RETENTION_DAYS:-30}"
MIN_KEEP="${BACKUP_MIN_KEEP:-3}"
LOG="$REPO_ROOT/backups/backup-cron.log"
mkdir -p "$REPO_ROOT/backups"

log() { printf '%s %s\n' "$(date -u +%FT%TZ)" "$*" >> "$LOG"; }

# Validate both knobs before anything can delete a file. Bash arithmetic
# resolves an unknown identifier to 0, so an unvalidated BACKUP_MIN_KEEP=abc
# silently becomes "keep zero" — the retention floor disappears exactly when
# someone thought they were configuring it. Refuse instead of guessing.
for _var in RETENTION_DAYS MIN_KEEP; do
  case "${!_var}" in
    *[!0-9]* | "")
      log "FAILED: BACKUP_${_var} must be a non-negative integer, got: '${!_var}'"
      exit 2 ;;
  esac
done
unset _var

# Any unexpected failure must land in the log, not vanish into cron's void.
trap 'log "FAILED: unexpected error at line $LINENO (exit $?)"' ERR

log "=== backup run starting ==="

# Have backup.sh name the archive it produced. Picking "the newest tarball in
# backups/" instead would let a concurrent or hand-started backup slip in
# between the run and the check, so this wrapper would verify — and vouch for —
# an archive it did not create, while its own went unchecked.
PATH_FILE="$(mktemp)"
trap 'rm -f "$PATH_FILE"' EXIT

if ! NO_COLOR=1 ORKESTRA_BACKUP_PATH_FILE="$PATH_FILE" ./backup.sh --yes all >> "$LOG" 2>&1; then
  log "FAILED: backup.sh exited non-zero — no usable backup was written"
  exit 1
fi

LATEST="$(cat "$PATH_FILE")"
if [ -z "$LATEST" ] || [ ! -f "$LATEST" ]; then
  log "FAILED: backup.sh reported success but named no tarball we can find"
  exit 1
fi

# Verify rather than trust: the manifest must list every component, and the
# Mongo archive must actually contain bytes.
COMPONENTS=$(tar xzf "$LATEST" -O ./manifest.json | tr -d ' \n')
for c in mongodb redis rustfs secrets; do
  case "$COMPONENTS" in
    *"\"$c\""*) ;;
    *) log "FAILED: $LATEST manifest is missing component '$c'"; exit 1 ;;
  esac
done

if [ "$(tar xzf "$LATEST" -O ./mongodb/mongo.archive.gz | wc -c)" -lt 1024 ]; then
  log "FAILED: $LATEST mongo archive is empty or truncated"
  exit 1
fi

# The tarball carries docker/.env and docker/keys/* — DB passwords, the KMS
# master key and the JWT private key. It must never be group- or world-readable.
# backup.sh itself now guarantees this mode (umask 077 + an explicit chmod
# right after `tar czf`), so this is belt-and-braces, not the only guard —
# kept anyway because it's one line and this wrapper is the thing actually
# scheduled on the host; a future backup.sh regression that reintroduces a
# readable tarball still gets caught here before cron leaves it on disk.
chmod 600 "$LATEST"
log "OK: $LATEST ($(du -h "$LATEST" | cut -f1)) components=$COMPONENTS"

# Prune by age, but never below a floor: age-only pruning ran here once
# already and deleted the single pre-existing backup because it was 67 days
# old, beyond the 30-day retention window — that specific run couldn't prune
# to *zero* only because it runs after a fresh verified-good backup just
# landed, but a host that's been down for a while (or has retention set
# aggressively) could still legitimately have zero recent-enough backups and
# lose everything older in one pass. MIN_KEEP guarantees at least that many
# backups survive regardless of age, on top of the age-based rule.
# MIN_KEEP is read and validated at the top, with RETENTION_DAYS.

mapfile -t ALL_SORTED < <(find "$REPO_ROOT/backups" -maxdepth 1 -name 'orkestra-backup-*.tar.gz' -printf '%T@ %p\n' | sort -n | cut -d' ' -f2-)
TOTAL=${#ALL_SORTED[@]}
KEEPABLE=$((TOTAL - MIN_KEEP))

DELETED=0
if [ "$KEEPABLE" -gt 0 ]; then
  i=0
  for f in "${ALL_SORTED[@]}"; do
    [ "$i" -ge "$KEEPABLE" ] && break
    i=$((i + 1))
    match="$(find "$f" -maxdepth 0 -mtime "+${RETENTION_DAYS}" -delete -print)" || true
    [ -n "$match" ] && DELETED=$((DELETED + 1))
  done
fi
log "pruned $DELETED backup(s) older than ${RETENTION_DAYS}d (kept at least ${MIN_KEEP} of ${TOTAL} regardless of age)"
log "=== backup run finished ==="
