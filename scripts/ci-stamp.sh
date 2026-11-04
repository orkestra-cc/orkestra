#!/usr/bin/env bash
# ci-stamp.sh — remembers green `make ci` runs by git tree, so the pre-push
# hook does not run a second full gate on content that already passed one.
#
#   ci-stamp.sh write [<surface>...]          after a green gate (`make ci`, `make ci-all`)
#   ci-stamp.sh check <commit> [<surface>...] exit 0 when <commit>'s tree has a stamp
#                                             covering every listed surface
#   ci-stamp.sh prepush                       the pre-push hook entry
#
# A stamp is the file <git-common-dir>/ci-stamps/<tree sha>, one surface per
# line (`make ci-surfaces` names them). It lives in the common dir, so every
# linked worktree of the clone sees it; nothing is ever pushed. `write` stamps
# only a clean tree (no modified, staged or untracked file): `make ci` checks
# the working tree, and only a clean one is exactly HEAD's tree. Keyed by tree,
# not commit, so a reworded or rebased commit with the same content still
# matches. The newest $CI_STAMP_KEEP (200) stamps are kept.
#
# `prepush` reads the pushed commit from PRE_COMMIT_TO_REF (set by pre-commit
# for pre-push hooks) and skips the gate when that tree is stamped for every
# surface `make ci-surfaces` lists now. Otherwise, or with CI_GATE_FORCE=1, it
# runs the gate: through `ci-gate` (the host's gate queue, octolabs-worktree
# scripts/host/ci-gate) when that is on PATH, else plain `make ci`.
set -uo pipefail

stamp_dir() {
    local common
    common=$(git rev-parse --path-format=absolute --git-common-dir 2>/dev/null) || return 1
    printf '%s/ci-stamps' "$common"
}

cmd_write() {
    local dir tree tmp
    # A stamp is an optimisation: never fail the gate that calls this.
    if [ -n "$(git status --porcelain --untracked-files=normal 2>/dev/null)" ]; then
        echo "ci-stamp: not stamped (uncommitted changes: the gate checked the working tree, not HEAD)"
        return 0
    fi
    dir=$(stamp_dir) && tree=$(git rev-parse 'HEAD^{tree}' 2>/dev/null) || {
        echo "ci-stamp: not stamped (no git tree)"; return 0; }
    mkdir -p "$dir" 2>/dev/null || return 0
    tmp="$dir/.$tree.$$"
    { printf '%s\n' "$@"; } > "$tmp" 2>/dev/null && mv -f "$tmp" "$dir/$tree" 2>/dev/null || {
        rm -f "$tmp"; return 0; }
    echo "ci-stamp: tree ${tree:0:12} green for: ${*:-(no surface)}"
    # shellcheck disable=SC2012 # stamp names are hex shas
    ls -1t "$dir" 2>/dev/null | tail -n +"$(( ${CI_STAMP_KEEP:-200} + 1 ))" \
        | while read -r old; do rm -f "$dir/$old"; done
    return 0
}

cmd_check() {
    local commit=$1 dir tree s
    shift
    dir=$(stamp_dir) || return 1
    tree=$(git rev-parse --verify --quiet "$commit^{tree}") || return 1
    [ -f "$dir/$tree" ] || return 1
    for s in "$@"; do
        grep -qxF -- "$s" "$dir/$tree" || return 1
    done
    return 0
}

run_gate() {
    if command -v ci-gate >/dev/null 2>&1; then
        exec ci-gate make ci
    fi
    exec make ci
}

cmd_prepush() {
    local to=${PRE_COMMIT_TO_REF:-} surfaces
    if [ "${CI_GATE_FORCE:-}" = 1 ]; then
        echo "ci-stamp: CI_GATE_FORCE=1, running the gate"
        run_gate
    fi
    # A branch deletion pushes the zero sha: nothing to check against.
    if [ -n "$to" ] && ! [[ "$to" =~ ^0+$ ]]; then
        # shellcheck disable=SC2207 # surfaces are single words
        surfaces=($(make -s --no-print-directory ci-surfaces 2>/dev/null)) || surfaces=()
        if cmd_check "$to" "${surfaces[@]}"; then
            echo "ci-stamp: tree of ${to:0:12} already passed make ci (${surfaces[*]:-no surface}); gate skipped"
            echo "          rerun it with CI_GATE_FORCE=1 git push"
            exit 0
        fi
    fi
    run_gate
}

case "${1:-}" in
    write) shift; cmd_write "$@" ;;
    check) shift; [ $# -ge 1 ] || { echo "usage: ci-stamp.sh check <commit> [<surface>...]" >&2; exit 2; }; cmd_check "$@" ;;
    prepush) cmd_prepush ;;
    *) echo "usage: ci-stamp.sh write [<surface>...] | check <commit> [<surface>...] | prepush" >&2; exit 2 ;;
esac
