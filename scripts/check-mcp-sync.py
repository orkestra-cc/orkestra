#!/usr/bin/env python3
"""Check that Claude Code and Codex project MCP definitions match."""

import json
import sys
import tomllib
from pathlib import Path
from typing import Any


def normalized_servers(servers: dict[str, Any]) -> dict[str, dict[str, Any]]:
    normalized = {}
    for name, config in servers.items():
        unsupported = set(config) - {"command", "args", "env", "type"}
        if config.get("type", "stdio") != "stdio":
            unsupported.add("type")
        if unsupported:
            fields = ", ".join(sorted(unsupported))
            raise ValueError(f"MCP server {name!r} has unsupported fields: {fields}")
        normalized[name] = {
            "command": config.get("command"),
            "args": config.get("args", []),
            "env": config.get("env", {}),
        }
    return normalized


def main() -> int:
    if len(sys.argv) == 1:
        claude_path = Path(".mcp.json")
        codex_path = Path(".codex/config.toml")
    elif len(sys.argv) == 3:
        claude_path = Path(sys.argv[1])
        codex_path = Path(sys.argv[2])
    else:
        print(f"Usage: {Path(sys.argv[0]).name} [.mcp.json .codex/config.toml]", file=sys.stderr)
        return 2

    try:
        with claude_path.open(encoding="utf-8") as source:
            claude = normalized_servers(json.load(source).get("mcpServers", {}))
        with codex_path.open("rb") as source:
            codex = normalized_servers(tomllib.load(source).get("mcp_servers", {}))
    except (OSError, ValueError) as error:
        print(error, file=sys.stderr)
        return 2

    if claude == codex:
        print("Claude Code and Codex project MCP definitions are in sync.")
        return 0

    for name in sorted(claude.keys() | codex.keys()):
        if name not in claude:
            print(f"MCP server {name!r} exists only in Codex configuration.", file=sys.stderr)
        elif name not in codex:
            print(f"MCP server {name!r} exists only in Claude Code configuration.", file=sys.stderr)
        elif claude[name] != codex[name]:
            print(f"MCP server {name!r} has different command, args, or env values.", file=sys.stderr)
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
