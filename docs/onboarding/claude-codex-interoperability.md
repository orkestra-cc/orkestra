# Claude Code and Codex interoperability

This repository keeps shared project instructions, skills, and MCP servers so
contributors can switch between Claude Code and Codex without maintaining
duplicate content.

## Shared project instructions

`CLAUDE.md` is the canonical instruction file. At the repository root,
`AGENTS.md` is a symbolic link to it. Nested `CLAUDE.md` files are discovered
by Codex through `project_doc_fallback_filenames` in `.codex/config.toml`.

Edit `CLAUDE.md` only; do not replace `AGENTS.md` with a copied file.

## Shared skills and references

Skills live under `.claude/skills/`. Codex discovers the same directories
through the `.agents/skills` symbolic link. References, scripts, and assets
inside each skill therefore remain shared automatically.

Add or update a repository skill in `.claude/skills/<name>/`; do not create a
second Codex-specific copy.

## Shared MCP definitions

Claude Code reads `.mcp.json`. Codex reads `.codex/config.toml`. Both files
declare the same project MCP servers, but their schemas differ, so a literal
symbolic link is not possible. When adding, removing, or changing a project MCP
server, update both files in the same change.

Credentials must come from environment variables or an authentication flow;
never commit tokens or passwords to either file.

Run `make mcp-check` after editing either file. The same semantic comparison
runs from pre-commit whenever `.mcp.json` or `.codex/config.toml` is staged,
from `make ci` when the shared MCP configuration or checker changes, and in
the dedicated MCP configuration GitHub Actions workflow.

The shared `integrated-browser-mcp` entry expects the
`thimo.integrated-browser-mcp` VS Code extension to be installed. The extension
places its stdio server under `~/.integrated-browser-mcp/`; the project config
resolves that location through `$HOME` and keeps the repository free of
machine-specific absolute paths.

## User plugins, commands, agents, and history

These are not repository-portable through symbolic links. In a local Codex CLI
session, run:

```text
/import
```

Choose **Claude Code**, then select the user setup and project items to import.
Codex can convert supported Claude Code plugins, slash commands, subagents,
hooks, MCP configuration, instructions, skills, and recent chats. Review any
connection that requires OAuth, custom headers, environment variables, or
different permissions. Run the import again after changing user-level Claude
Code configuration; repository instructions and skills do not need re-importing
because they are shared directly.

The `/import` command must be run from an idle local Codex CLI session. It is
not available during a task, in a remote session, or through a local app-server
daemon.

## Verification

Start a fresh session after changing agent configuration. In Codex, verify MCP
servers with `/mcp` or `codex mcp list`, and verify available skills with
`/skills`. Ask the agent to list its active instruction sources from the root
and from a nested module directory to confirm that the expected `CLAUDE.md`
chain is loaded.
