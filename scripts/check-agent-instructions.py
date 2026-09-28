#!/usr/bin/env python3
"""Check that the repository has no CLAUDE.md-style instruction file.

AGENTS.md is the only project-instructions filename. Claude Code reads it only
while no CLAUDE.md, .claude/CLAUDE.md or CLAUDE.local.md exists in the working
directory or above it; one such file, even a symbolic link to AGENTS.md, makes
it read CLAUDE.md files only and drop every AGENTS.md. See
docs/onboarding/claude-codex-interoperability.md.
"""

import subprocess
import sys
from pathlib import PurePosixPath

FORBIDDEN = {"CLAUDE.md", "CLAUDE.local.md"}


def candidate_paths(root: str) -> list[str]:
    # Tracked files plus untracked ones that are not ignored: a CLAUDE.md that
    # /init just wrote is caught before it is staged.
    output = subprocess.run(
        ["git", "-C", root, "ls-files", "--cached", "--others", "--exclude-standard", "-z"],
        check=True,
        capture_output=True,
    ).stdout
    return [path for path in output.decode().split("\0") if path]


def main() -> int:
    root = sys.argv[1] if len(sys.argv) > 1 else "."
    offenders = sorted(
        path for path in candidate_paths(root) if PurePosixPath(path).name in FORBIDDEN
    )
    if not offenders:
        print("Agent instructions: no CLAUDE.md files, AGENTS.md only")
        return 0

    print("Agent instructions: CLAUDE.md-style files found:", file=sys.stderr)
    for path in offenders:
        print(f"  {path}", file=sys.stderr)
    print(
        "\nClaude Code reads AGENTS.md only while no CLAUDE.md or CLAUDE.local.md\n"
        "exists in the working directory or above it, so each of these files\n"
        "hides the AGENTS.md instructions. Move the content into the matching\n"
        "AGENTS.md and delete the file. Personal instructions belong in\n"
        "~/.claude/CLAUDE.md. See docs/onboarding/claude-codex-interoperability.md.",
        file=sys.stderr,
    )
    return 1


if __name__ == "__main__":
    sys.exit(main())
