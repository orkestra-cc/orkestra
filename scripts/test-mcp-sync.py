#!/usr/bin/env python3

import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


CHECKER = Path(__file__).with_name("check-mcp-sync.py")


class McpSyncTest(unittest.TestCase):
    def run_checker(self, claude: dict, codex: str) -> subprocess.CompletedProcess[str]:
        with tempfile.TemporaryDirectory() as temp_dir:
            temp = Path(temp_dir)
            claude_path = temp / ".mcp.json"
            codex_path = temp / "config.toml"
            claude_path.write_text(json.dumps(claude), encoding="utf-8")
            codex_path.write_text(codex, encoding="utf-8")
            return subprocess.run(
                [sys.executable, str(CHECKER), str(claude_path), str(codex_path)],
                check=False,
                capture_output=True,
                text=True,
            )

    def test_accepts_equivalent_server_definitions(self) -> None:
        result = self.run_checker(
            {
                "mcpServers": {
                    "example": {
                        "command": "example-mcp",
                        "args": ["--mode", "safe"],
                        "env": {"EXAMPLE_HOST": "localhost"},
                    }
                }
            },
            """
[mcp_servers.example]
command = "example-mcp"
args = ["--mode", "safe"]

[mcp_servers.example.env]
EXAMPLE_HOST = "localhost"
""",
        )

        self.assertEqual(result.returncode, 0, result.stderr)

    def test_rejects_missing_server(self) -> None:
        result = self.run_checker(
            {"mcpServers": {"claude-only": {"command": "example-mcp"}}},
            "",
        )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("claude-only", result.stderr)

    def test_rejects_different_server_configuration(self) -> None:
        result = self.run_checker(
            {
                "mcpServers": {
                    "example": {"command": "example-mcp", "args": ["--one"]}
                }
            },
            """
[mcp_servers.example]
command = "example-mcp"
args = ["--two"]
""",
        )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("example", result.stderr)

    def test_rejects_fields_the_checker_does_not_compare(self) -> None:
        result = self.run_checker(
            {
                "mcpServers": {
                    "example": {"type": "http", "url": "https://example.com/mcp"}
                }
            },
            """
[mcp_servers.example]
url = "https://different.example.com/mcp"
""",
        )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("unsupported fields", result.stderr)


if __name__ == "__main__":
    unittest.main()
