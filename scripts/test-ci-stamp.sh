#!/usr/bin/env bash
# Plain-bash tests for scripts/ci-stamp.sh (no framework). Builds throwaway
# Git repos, so it sheds an inherited GIT_DIR first (see
# test-script-tests-hermetic.sh, which also runs this suite).
set -uo pipefail
# shellcheck disable=SC2046
unset $(git rev-parse --local-env-vars)
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STAMP="$DIR/ci-stamp.sh"

pass=0; fail=0
check() {  # check <desc> <expected> <actual>
    if [ "$2" = "$3" ]; then
        pass=$((pass + 1)); printf '  ok   %s\n' "$1"
    else
        fail=$((fail + 1)); printf '  FAIL %s\n       expected: [%s]\n       actual:   [%s]\n' "$1" "$2" "$3"
    fi
}
status() { "$@" >/dev/null 2>&1; echo $?; }

tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
export GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@t GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@t
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1

repo="$tmp/repo"
git init -q -b main "$repo"
cd "$repo" || exit 1
echo one > a.txt
git add a.txt && git commit -q -m one
c1=$(git rev-parse HEAD)
dir="$repo/.git/ci-stamps"

# --- write ---
"$STAMP" write backend frontend-admin >/dev/null
check "clean tree is stamped" "backend
frontend-admin" "$(cat "$dir/$(git rev-parse 'HEAD^{tree}')" 2>/dev/null)"

echo dirty >> a.txt
out=$("$STAMP" write backend)
check "dirty tree is not stamped" "1" "$(ls "$dir" | wc -l | tr -d ' ')"
check "dirty tree says why" "1" "$(grep -c 'uncommitted' <<<"$out")"
git checkout -q a.txt
echo new > untracked.txt
check "untracked file blocks the stamp" "0" "$(status "$STAMP" write backend)"
check "untracked: still one stamp" "1" "$(ls "$dir" | wc -l | tr -d ' ')"
rm untracked.txt

# --- check ---
check "check: stamped surfaces"          "0" "$(status "$STAMP" check "$c1" backend)"
check "check: both surfaces"             "0" "$(status "$STAMP" check "$c1" backend frontend-admin)"
check "check: no surface needed"         "0" "$(status "$STAMP" check "$c1")"
check "check: surface not covered"       "1" "$(status "$STAMP" check "$c1" frontend-public)"
check "check: surface prefix is no match" "1" "$(status "$STAMP" check "$c1" front)"
echo two > a.txt && git commit -q -am two
c2=$(git rev-parse HEAD)
check "check: other tree has no stamp"   "1" "$(status "$STAMP" check "$c2" backend)"
check "check: unknown commit"            "1" "$(status "$STAMP" check deadbeef backend)"

# Same content, new commit (reword): the tree matches.
git reset -q --hard "$c1"
git commit -q --amend -m reworded
check "check: reworded commit, same tree" "0" "$(status "$STAMP" check HEAD backend)"

# --- the stamp lives in the common dir: a linked worktree sees it ---
git worktree add -q "$tmp/wt" -b feature "$c1"
check "check from a linked worktree" "0" "$(cd "$tmp/wt" && status "$STAMP" check HEAD backend)"

# --- prepush, with a fake `make` that records calls and lists surfaces ---
bin="$tmp/bin"; mkdir -p "$bin"
cat > "$bin/make" <<EOF
#!/bin/sh
case "\$*" in
  *ci-surfaces*) echo "\${FAKE_SURFACES:-}" ;;
  *) echo "make \$*" >> "$tmp/make.log" ;;
esac
EOF
chmod +x "$bin/make"
prepush() { PATH="$bin:/usr/bin:/bin" PRE_COMMIT_TO_REF="$1" FAKE_SURFACES="$2" CI_GATE_FORCE="${3:-}" "$STAMP" prepush; }
gates() { cat "$tmp/make.log" 2>/dev/null | wc -l | tr -d ' '; }

prepush "$c1" "backend" >/dev/null
check "prepush: stamped tree skips the gate" "0" "$(gates)"
prepush "$c1" "backend frontend-public" >/dev/null
check "prepush: a new surface runs the gate" "1" "$(gates)"
prepush "$c2" "backend" >/dev/null
check "prepush: unstamped tree runs the gate" "2" "$(gates)"
prepush "$c1" "backend" 1 >/dev/null
check "prepush: CI_GATE_FORCE=1 runs the gate" "3" "$(gates)"
prepush "0000000000000000000000000000000000000000" "backend" >/dev/null
check "prepush: zero sha (deletion) runs the gate" "4" "$(gates)"
prepush "" "backend" >/dev/null
check "prepush: no PRE_COMMIT_TO_REF runs the gate" "5" "$(gates)"
check "prepush: the gate is make ci" "make ci" "$(tail -n 1 "$tmp/make.log")"

# ci-gate on PATH: the gate goes through it.
cat > "$bin/ci-gate" <<EOF
#!/bin/sh
echo "ci-gate \$*" >> "$tmp/make.log"
EOF
chmod +x "$bin/ci-gate"
prepush "$c2" "backend" >/dev/null
check "prepush: ci-gate wraps the gate when installed" "ci-gate make ci" "$(tail -n 1 "$tmp/make.log")"

# --- pruning keeps the newest CI_STAMP_KEEP ---
for i in 1 2 3; do echo "$i" > a.txt; git commit -q -am "p$i"; CI_STAMP_KEEP=2 "$STAMP" write backend >/dev/null; done
check "prune keeps CI_STAMP_KEEP stamps" "2" "$(ls "$dir" | wc -l | tr -d ' ')"
check "prune keeps the newest" "0" "$(status "$STAMP" check HEAD backend)"

echo
echo "ci-stamp: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
