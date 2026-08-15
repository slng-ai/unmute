# Contract: `unmute skill`

The CLI surface this feature adds. This is the contract a user and a test both
read. Behaviour not written here is not promised.

## Shape

```text
unmute skill install [flags]

Flags:
  --agent strings   Which assistants to install for: claude, codex, cursor,
                    copilot, all. Repeatable. Default: all.
  --dir string      Project directory to install into. Default: the current
                    directory.
  --force           Overwrite files that were changed after install.
```

`unmute skill` with no subcommand prints help and exits 0, matching how the root
behaves with no arguments.

## Guarantees

- **No network.** The command makes no request. It works on a machine that has
  never been online.
- **No package touched.** It never reads, writes, or validates an Unmute
  package. It can run in a directory that holds no package at all.
- **Two destinations only.** It creates and writes inside
  `.agents/skills/unmute/` and `.claude/skills/unmute/`, and nowhere else. It
  does not create or edit `AGENTS.md`, `.github/`, `.cursor/`, or any file the
  user owns.
- **Whole or nothing.** A destination that fails part way through is rolled back
  to what it was, so a failed install never resembles a good one.
- **Everything is reported.** Every file is printed with what happened to it:
  written, updated, upgraded, left alone, or refused.

## Exit codes

| Code | When |
|---|---|
| 0 | every requested destination is current, whether it was written or already matched |
| 1 | an unknown `--agent`, an unwritable directory, or a refusal on locally changed files without `--force` |

Warnings go to stderr and still exit 0, per the repository's command rules.

## Behaviour

### Fresh install

Writes each destination, writes its manifest, prints where the files landed and
what to do next. The next step named is running an assistant and asking it for
an agent, not another command.

### Already current

Prints that each destination is already current and changes nothing. Exit 0.
This must be distinguishable in the output from a fresh install, because "did
it do anything" is the question a second run is asked to answer.

### Newer CLI than the installed skill

Writes the new files and reports the version it upgraded from. No prompt, since
nothing the user wrote is being lost.

### Locally changed files

Refuses. Names every file whose hash no longer matches its manifest, in one
message rather than one per run, and says that `--force` overwrites them. Exit 1.
Nothing is written.

### Unknown assistant

Fails naming the value it did not recognise and listing every supported name.
Exit 1. It never falls back to `all`.

### Unwritable directory

Fails with the path and the underlying reason, wrapped with `%w`. Exit 1.

## Output shape

Plain lines, one per file, then a short next step. No colour literal appears in
this command's code; anything styled goes through `internal/style`, per the
repository rule.

```text
unmute skill install

  .agents/skills/unmute/
    SKILL.md                      written
    references/package.md         written
    ...
  .claude/skills/unmute/
    SKILL.md                      written

Installed the Unmute skill for claude, codex, cursor, copilot.
Commit these files so your team's assistants get them too.
Next: ask your assistant to build a voice agent, in a sentence.
```

## What this contract does not cover

- The content of the bundle. That is [skill-bundle.md](skill-bundle.md).
- Uninstalling. Deleting the two directories is the uninstall, and the command
  says so rather than shipping a subcommand for `rm -r`.
- A global install into a user's home directory. Out of scope for this version,
  per the spec's Assumptions. The skill is meant to travel in the repository so
  a team shares one.
