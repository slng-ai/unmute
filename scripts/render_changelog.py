#!/usr/bin/env python3
"""Render one published GitHub Release into the docs site changelog.

The GitHub Release is the single source of truth. GoReleaser already writes it
from the tag message and the git subjects, so this script derives a page entry
from it rather than asking anybody to keep a second changelog by hand.

Run by .github/workflows/changelog.yml on `release: published`, and by hand to
backfill or to read a diff:

    python3 scripts/render_changelog.py v0.2.5
    python3 scripts/render_changelog.py v0.2.5 --dry-run

Re-running for a tag already on the page is a success that writes nothing.
Failed releases get re-run in this repository, so that case is the one that
matters most.

Standard library only, on purpose. Nothing here needs more.
"""

from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from pathlib import Path

CHANGELOG = Path("docs-site/changelog.mdx")
VERSION_SNIPPET = Path("docs-site/snippets/unmute-version.mdx")
RELEASE_URL = "https://github.com/slng-ai/unmute/releases/tag/{tag}"

# The script inserts directly after this marker, so the page's hand written lead
# paragraph stays above every entry. Inserting after the frontmatter instead
# would push that lead below the newest release.
INSERT_MARKER = "{/* changelog:entries */}"

# The one line the version marker carries. The rewrite is a substitution on this
# exact shape, so there is nothing to parse.
MARKER_LINE = re.compile(r"(?m)^(export const unmuteVersion = ')([^']*)(';)$")

SEMVER = re.compile(r"^v(\d+)\.(\d+)\.(\d+)$")

# A release body opens in one of three ways, measured across all nine shipped
# releases. Each pattern below is here because a real body needed it.
#
# v0.1.0's annotated tag message was a merge commit subject, because the tag was
# cut on a merge. That subject is not a change anybody wants to read.
MERGE_SUBJECT = re.compile(r"^Merge (?:pull request|branch|remote-tracking) .*$")

# v0.1.1, v0.1.2 and v0.2.0 open with a level one heading naming the version.
# v0.2.1 through v0.2.5 open with the version joined to a summary by an em dash,
# a colon or a hyphen. The version in the line is not required to match the tag:
# the v0.1.1 release titles itself "Unmute v0.1.0", and repeating that mistake on
# the docs site would be worse than dropping the line.
TITLE_LINE = re.compile(
    r"^\s*#*\s*(?:Unmute\s+)?v\d+\.\d+\.\d+\s*"
    r"(?:[:\-–—]\s*(?P<rest>\S.*?))?\s*$"
)

# Everything from here down is GoReleaser's generated list of 40 character commit
# hashes. The linked release carries it in full, next to the binaries.
COMMIT_SECTION = re.compile(r"^\s*#{1,6}\s*Changelog\s*$", re.IGNORECASE)

# Any markdown heading, with its indentation kept.
HEADING = re.compile(r"^(?P<indent>[ \t]*)#{1,6}[ \t]+(?P<text>.+?)[ \t]*#*[ \t]*$")

FENCE = re.compile(r"^\s*(?:```|~~~)")

# Month names spelled out rather than taken from strftime, which follows the
# locale. A runner with a different locale would otherwise silently write a
# different label than a laptop does.
MONTHS = (
    "January",
    "February",
    "March",
    "April",
    "May",
    "June",
    "July",
    "August",
    "September",
    "October",
    "November",
    "December",
)


class RenderError(Exception):
    """Something was wrong with the input or the files. Always names the file."""


def load_release(tag: str, from_json: Path | None) -> dict:
    """Read the release from gh, or from a file when testing offline."""
    if from_json is not None:
        try:
            payload = json.loads(from_json.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError) as err:
            raise RenderError(f"read {from_json}: {err}") from err
    else:
        command = ["gh", "release", "view", tag, "--json", "tagName,body,publishedAt"]
        try:
            done = subprocess.run(
                command, capture_output=True, text=True, check=True
            )
        except FileNotFoundError as err:
            raise RenderError("gh is not installed, and it is how the release is read") from err
        except subprocess.CalledProcessError as err:
            raise RenderError(
                f"gh could not read release {tag}: {err.stderr.strip() or err}"
            ) from err
        payload = json.loads(done.stdout)

    for field in ("tagName", "body", "publishedAt"):
        if field not in payload:
            raise RenderError(f"the release payload for {tag} has no {field!r}")
    if payload["tagName"] != tag:
        raise RenderError(
            f"asked for {tag} but the payload names {payload['tagName']!r}"
        )
    return payload


def month_label(published: str) -> str:
    """Turn an RFC 3339 timestamp into the entry label, for example August 2026."""
    match = re.match(r"^(\d{4})-(\d{2})-(\d{2})", published)
    if not match:
        raise RenderError(f"publishedAt {published!r} is not a date this can read")
    year, month = int(match.group(1)), int(match.group(2))
    if not 1 <= month <= 12:
        raise RenderError(f"publishedAt {published!r} names month {month}")
    return f"{MONTHS[month - 1]} {year}"


def split_fences(text: str) -> list[tuple[bool, str]]:
    """Split into (is_code, chunk) runs so a fenced block is never rewritten.

    One release body carries a fenced example. Bolding a heading inside it, or
    escaping a character inside it, would corrupt the example.
    """
    chunks: list[tuple[bool, str]] = []
    current: list[str] = []
    in_code = False
    for line in text.split("\n"):
        if FENCE.match(line):
            current.append(line)
            if in_code:
                chunks.append((True, "\n".join(current)))
                current = []
            else:
                if len(current) > 1:
                    chunks.append((False, "\n".join(current[:-1])))
                current = [line]
            in_code = not in_code
            continue
        current.append(line)
    if current:
        chunks.append((in_code, "\n".join(current)))
    return chunks


def strip_leading_title(body: str) -> tuple[str, str]:
    """Drop a merge subject and a version title line.

    Returns the remaining body and a lead sentence, which is the summary that
    followed the version on the title line. Empty when the title carried none.
    """
    lines = body.replace("\r\n", "\n").split("\n")
    lead = ""

    while lines and not lines[0].strip():
        lines.pop(0)
    while lines and MERGE_SUBJECT.match(lines[0].strip()):
        lines.pop(0)
        while lines and not lines[0].strip():
            lines.pop(0)
    if lines:
        match = TITLE_LINE.match(lines[0])
        if match:
            rest = (match.group("rest") or "").strip()
            if rest:
                lead = rest[0].upper() + rest[1:]
                if lead[-1] not in ".!?:":
                    lead += "."
            lines.pop(0)
            while lines and not lines[0].strip():
                lines.pop(0)
    return "\n".join(lines), lead


def cut_commit_section(body: str) -> str:
    """Drop the generated commit list. The linked release carries it."""
    out: list[str] = []
    in_code = False
    for line in body.split("\n"):
        if FENCE.match(line):
            in_code = not in_code
        elif not in_code and COMMIT_SECTION.match(line):
            break
        out.append(line)
    return "\n".join(out)


def headings_to_bold(chunk: str) -> str:
    """Turn every heading into bold text.

    The page's heading hierarchy is checked by internal/docsite: no level one
    heading, and no jump between levels. Release bodies start at different
    levels from each other, so a fixed demotion would still jump. Bold has no
    level, and it keeps the table of contents to one entry per release, which is
    what the Update label is for.
    """
    out = []
    for line in chunk.split("\n"):
        match = HEADING.match(line)
        if match:
            out.append(f"{match.group('indent')}**{match.group('text')}**")
        else:
            out.append(line)
    return "\n".join(out)


def escape_mdx(chunk: str) -> str:
    """Escape < and { outside inline code spans.

    MDX reads `<` as the start of a component and `{` as the start of an
    expression, and release bodies contain both in plain prose: `"<id>:<agent>"`
    and `{{placeholders}}`. Inside a backtick span MDX parses neither, so those
    are left alone and render as the author wrote them.
    """
    out = []
    for index, part in enumerate(chunk.split("`")):
        if index % 2 == 0:
            part = part.replace("<", "&lt;").replace("{", "&#123;")
        out.append(part)
    return "`".join(out)


def normalise_dashes(chunk: str) -> str:
    """Remove em and en dashes used as punctuation.

    The site is written without them. A dash between two numbers is a range, so
    it becomes "to"; anywhere else it is separating clauses, so it becomes a
    comma. Four of the nine bodies contain one.
    """
    # The optional # matters. v0.2.2 writes its pull request range as
    # "#92–#119", so the character after the dash is a hash, not a digit.
    chunk = re.sub(r"(?<=\d)\s*[–—]\s*(?=#?\d)", " to ", chunk)
    chunk = re.sub(r"(?<=\S)\s*[–—]\s*", ", ", chunk)
    chunk = re.sub(r"[–—]\s*", "", chunk)
    return re.sub(r",\s*,", ",", chunk)


def dedent(body: str) -> str:
    """Remove indentation the whole body shares.

    v0.2.0's release notes are indented by two spaces throughout, which would
    otherwise be the only entry on the page that is. Removing shared
    indentation cannot create a code block, which is why this dedents rather
    than re-indenting to a house style.
    """
    lines = body.split("\n")
    filled = [line for line in lines if line.strip()]
    if not filled:
        return body
    shared = min(len(line) - len(line.lstrip()) for line in filled)
    if shared == 0:
        return body
    return "\n".join(line[shared:] if line.strip() else line for line in lines)


def tidy(body: str) -> str:
    """Collapse blank line runs and strip trailing whitespace."""
    lines = [line.rstrip() for line in body.split("\n")]
    out: list[str] = []
    for line in lines:
        if not line and out and not out[-1]:
            continue
        out.append(line)
    return "\n".join(out).strip("\n")


def render_body(body: str) -> str:
    """Apply every transform, in the order that matters.

    Cutting the commit section runs before bolding, so the "## Changelog"
    heading is removed rather than turned into bold text. Stripping the title
    line runs before dash normalisation, so a title separator dash is dropped
    rather than turned into a comma.
    """
    body, lead = strip_leading_title(body)
    body = cut_commit_section(body)
    body = dedent(body)

    rendered = []
    for is_code, chunk in split_fences(body):
        if is_code:
            rendered.append(chunk)
            continue
        chunk = headings_to_bold(chunk)
        chunk = normalise_dashes(chunk)
        chunk = escape_mdx(chunk)
        rendered.append(chunk)
    body = tidy("\n".join(rendered))

    if lead:
        body = f"{lead}\n\n{body}".strip() if body else lead
    return body


def render_entry(tag: str, published: str, body: str) -> str:
    """Build the whole Update block for one release."""
    rendered = render_body(body)
    link = f"[Full notes and downloads]({RELEASE_URL.format(tag=tag)})"
    parts = [part for part in (rendered, link) if part]
    return (
        f'<Update label="{month_label(published)}" description="{tag}" tags={{["CLI"]}}>\n'
        "\n"
        + "\n\n".join(parts)
        + "\n"
        "\n"
        "</Update>\n"
    )


def insert_entry(page: str, entry: str) -> str:
    """Put the entry directly below the marker, so the newest is first."""
    if INSERT_MARKER not in page:
        raise RenderError(
            f"{CHANGELOG} has no {INSERT_MARKER!r} line, which is where entries go"
        )
    # No trailing newline of its own: whatever followed the marker already starts
    # with one, so adding another leaves a growing gap between entries.
    return page.replace(INSERT_MARKER, f"{INSERT_MARKER}\n\n{entry.rstrip()}", 1)


def bump_marker(snippet: str, tag: str) -> tuple[str, bool]:
    """Move the version marker forward, never back.

    Rendering an old release by hand after a newer one shipped would otherwise
    un-version the whole site. The gate would catch it, but not moving backwards
    at all is better than being caught.
    """
    match = MARKER_LINE.search(snippet)
    if not match:
        raise RenderError(
            f"{VERSION_SNIPPET} has no `export const unmuteVersion = '...';` line"
        )
    current, new = SEMVER.match(match.group(2)), SEMVER.match(tag)
    if not new:
        raise RenderError(f"{tag!r} is not a version of the shape v1.2.3")
    if current and tuple(map(int, current.groups())) >= tuple(map(int, new.groups())):
        return snippet, False
    return MARKER_LINE.sub(rf"\g<1>{tag}\g<3>", snippet, count=1), True


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    parser.add_argument("tag", help="the release tag, for example v0.2.5")
    parser.add_argument(
        "--from-json",
        type=Path,
        help="read the release from this file instead of calling gh",
    )
    parser.add_argument(
        "--dry-run", action="store_true", help="print what would change, write nothing"
    )
    parser.add_argument(
        "--repo-root",
        type=Path,
        default=Path(__file__).resolve().parent.parent,
        help="repository root, so the script runs from any directory",
    )
    args = parser.parse_args(argv)

    changelog = args.repo_root / CHANGELOG
    snippet_path = args.repo_root / VERSION_SNIPPET
    try:
        page = changelog.read_text(encoding="utf-8")
        snippet = snippet_path.read_text(encoding="utf-8")
    except OSError as err:
        print(f"render_changelog: {err}", file=sys.stderr)
        return 1

    try:
        if f'description="{args.tag}"' in page:
            print(f"{args.tag} is already on {CHANGELOG}; nothing to do")
            return 0

        release = load_release(args.tag, args.from_json)
        entry = render_entry(args.tag, release["publishedAt"], release["body"])
        page = insert_entry(page, entry)
        snippet, moved = bump_marker(snippet, args.tag)
    except RenderError as err:
        print(f"render_changelog: {err}", file=sys.stderr)
        return 1

    if args.dry_run:
        print(entry)
        print(f"would write {CHANGELOG}", file=sys.stderr)
        print(
            f"would {'move' if moved else 'leave'} the version marker", file=sys.stderr
        )
        return 0

    changelog.write_text(page, encoding="utf-8")
    snippet_path.write_text(snippet, encoding="utf-8")
    print(f"added {args.tag} to {CHANGELOG}")
    print(
        f"version marker {'now ' + args.tag if moved else 'left alone, it already names a newer release'}"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
