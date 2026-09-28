#!/usr/bin/env python3

import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


CHECKER = Path(__file__).with_name("check-agent-instructions.py")


class AgentInstructionsTest(unittest.TestCase):
    def run_checker(self, files: dict[str, str], ignore: str = "") -> subprocess.CompletedProcess[str]:
        with tempfile.TemporaryDirectory() as temp_dir:
            temp = Path(temp_dir)
            subprocess.run(["git", "init", "-q", str(temp)], check=True)
            if ignore:
                (temp / ".gitignore").write_text(ignore, encoding="utf-8")
            for name, content in files.items():
                path = temp / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(content, encoding="utf-8")
            return subprocess.run(
                [sys.executable, str(CHECKER), str(temp)],
                check=False,
                capture_output=True,
                text=True,
            )

    def test_accepts_agents_md_only(self) -> None:
        result = self.run_checker({"AGENTS.md": "root", "backend/AGENTS.md": "backend"})

        self.assertEqual(result.returncode, 0, result.stderr)

    def test_rejects_nested_claude_md(self) -> None:
        result = self.run_checker({"AGENTS.md": "root", "backend/CLAUDE.md": "backend"})

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("backend/CLAUDE.md", result.stderr)

    def test_rejects_claude_dir_and_local_files(self) -> None:
        result = self.run_checker(
            {"AGENTS.md": "root", ".claude/CLAUDE.md": "x", "CLAUDE.local.md": "y"}
        )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn(".claude/CLAUDE.md", result.stderr)
        self.assertIn("CLAUDE.local.md", result.stderr)

    def test_rejects_symlink_to_agents_md(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            temp = Path(temp_dir)
            subprocess.run(["git", "init", "-q", str(temp)], check=True)
            (temp / "AGENTS.md").write_text("root", encoding="utf-8")
            (temp / "CLAUDE.md").symlink_to("AGENTS.md")
            result = subprocess.run(
                [sys.executable, str(CHECKER), str(temp)],
                check=False,
                capture_output=True,
                text=True,
            )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("CLAUDE.md", result.stderr)

    def test_ignores_gitignored_files(self) -> None:
        result = self.run_checker(
            {"AGENTS.md": "root", "vendor/CLAUDE.md": "third party"}, ignore="vendor/\n"
        )

        self.assertEqual(result.returncode, 0, result.stderr)

    def test_allows_similar_names(self) -> None:
        result = self.run_checker({"AGENTS.md": "root", "docs/CLAUDE-notes.md": "x"})

        self.assertEqual(result.returncode, 0, result.stderr)


if __name__ == "__main__":
    unittest.main()
