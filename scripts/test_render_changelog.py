"""Offline regression checks for release sync: python3 -m unittest discover -s scripts."""

import contextlib
import io
import json
import tempfile
import unittest
from pathlib import Path

import render_changelog as renderer


class ChangelogTest(unittest.TestCase):
    def test_backfill_edit_retry_and_dry_run(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            page = root / renderer.CHANGELOG
            snippet = root / renderer.VERSION_SNIPPET
            snippet.parent.mkdir(parents=True)
            page.write_text("Intro\n\n" + renderer.INSERT_MARKER + "\n")
            snippet.write_text("export const unmuteVersion = 'v0.0.0';\n")
            payload = root / "release.json"

            def run(tag, body="Notes", *flags):
                payload.write_text(json.dumps({
                    "tagName": tag, "body": body,
                    "publishedAt": "2026-09-01T12:00:00Z",
                }))
                with contextlib.redirect_stdout(io.StringIO()):
                    with contextlib.redirect_stderr(io.StringIO()):
                        return renderer.main([
                            tag, "--repo-root", str(root),
                            "--from-json", str(payload), *flags,
                        ])

            for tag in ("v0.4.2", "v0.3.2", "v0.4.1", "v0.10.0"):
                self.assertEqual(run(tag), 0)
            text = page.read_text()
            tags = ("v0.10.0", "v0.4.2", "v0.4.1", "v0.3.2")
            positions = [text.index(f'description="{tag}"') for tag in tags]
            self.assertEqual(positions, sorted(positions))
            self.assertIn("'v0.10.0'", snippet.read_text())

            self.assertEqual(run("v0.4.1", "Corrected notes"), 0)
            self.assertEqual(page.read_text().count('description="v0.4.1"'), 1)
            self.assertIn("Corrected notes", page.read_text())
            before = page.read_bytes(), snippet.read_bytes(), page.stat().st_mtime_ns
            self.assertEqual(run("v0.4.1", "Corrected notes"), 0)
            self.assertEqual(run("v0.11.0", "Preview", "--dry-run"), 0)
            self.assertEqual(run("v0.11.0-rc.1"), 1)
            self.assertEqual(
                before, (page.read_bytes(), snippet.read_bytes(), page.stat().st_mtime_ns)
            )

    def test_release_notes_are_mdx_safe(self):
        body = renderer.render_body(
            "v0.4.2 — use <name> and {{value}} — safely\n\n"
            "## Highlights\n\nRead <name> and {{value}} or `{{code}}`.\n\n"
            "```yaml\n# Changelog\nvalue: '{{code}}'\n```\n\n"
            "## Changelog\n* " + "a" * 40 + " internal commit"
        )
        self.assertIn("Use &lt;name> and &#123;&#123;value}}, safely.", body)
        self.assertIn("**Highlights**", body)
        self.assertIn("`{{code}}`", body)
        self.assertIn("```yaml\n# Changelog\nvalue: '{{code}}'\n```", body)
        self.assertNotIn("a" * 40, body)


if __name__ == "__main__":
    unittest.main()
