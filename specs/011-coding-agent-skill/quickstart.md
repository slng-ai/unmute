# Quickstart: validating the coding-agent skill

How to prove this feature works, end to end. Every step is runnable. Nothing
here needs Python, and nothing here needs a network.

## Prerequisites

- Go 1.24, as pinned in `go.mod`
- `make build` produces `./bin/unmute`
- One of the four assistants, for the manual checks in step 5

## 1. The automated gate

```sh
make fmt
make lint
make build
make test
```

`make test` must pass with zero Python and must include the new package:

```sh
go test ./internal/skill/... ./internal/cli/... -v
```

Nine bundle invariants and the command tests all live in the default suite. If
any agreement test is red, the bundle states something the code no longer
supports, and the fix is the bundle, not the test.

## 2. A fresh install

```sh
cd "$(mktemp -d)"
/path/to/unmute skill install
```

Expected:

- `.agents/skills/unmute/SKILL.md` and twelve files under `references/`
- `.claude/skills/unmute/SKILL.md`
- `.unmute-manifest.json` in both directories
- Every file printed as `written`
- Exit code 0
- No other file or directory created. Check with `ls -a`, and confirm there is
  no `AGENTS.md`, no `.github/`, no `.cursor/`.

Prove it needed no network:

```sh
# with networking disabled on the machine, or in an offline container
unmute skill install
```

Same result. This is FR-002.

## 3. Re-running is honest

```sh
unmute skill install
```

Expected: every file reported as already current, nothing rewritten, exit 0. The
output must read differently from step 2, because "did that do anything" is the
question this run answers.

Now change a file and try again:

```sh
echo "my own note" >> .agents/skills/unmute/references/tools.md
unmute skill install
```

Expected: refusal, exit 1, nothing written, and the message names
`references/tools.md` specifically. Then:

```sh
unmute skill install --force
```

Expected: the file is restored and reported as overwritten.

## 4. Selecting and mis-spelling an assistant

```sh
cd "$(mktemp -d)"
unmute skill install --agent codex
ls -a          # .agents/skills only, no .claude
```

```sh
unmute skill install --agent gemini
```

Expected: exit 1, a message naming `gemini` and listing `claude`, `codex`,
`cursor`, `copilot`, `all`. Nothing written. It must not fall back to `all`.

## 5. The part only a human can check

Automated tests hold the lists. They cannot tell you whether an assistant
actually builds a good agent. Run these by hand before calling the feature done.

**5a. It activates.** In a fresh project with the skill installed, start your
assistant and ask, in one sentence, for a voice agent that books salon
appointments. The assistant should reach for the skill without being told the
word "unmute".

**5b. It builds something that validates.** The package it writes should pass:

```sh
unmute validate
```

Zero errors on the first attempt, or one self-correction round where the
assistant reads the file, line, and column and fixes it rather than guessing.
This is SC-002, and the spec's bar is 8 of 10 briefs clean on the first attempt.

**5c. It says what it decided.** The reply must name the target it chose and the
models it bound. Default with no vendors named: SLNG to listen and speak, OpenAI
to think.

**5d. It refuses rather than invents.** Ask for a model vendor that has no
catalogue entry, and for a per-task model override on Pipecat. Both should be
refused by name, before files are written. An invented field here is the
failure mode this whole feature exists to prevent.

**5e. It sounds right.** Open the instructions file it wrote. No markdown
formatting, no bullet lists, no raw URLs, no unspoken digits.

**5f. You can speak to it.**

```sh
unmute dev
```

A green validation is not a good call. This is the real definition of done.

**5g. Two assistants, one body of text.** With both destinations installed,
confirm the pointer at `.claude/skills/unmute/SKILL.md` sends the reader to the
canonical bundle, and that there is exactly one copy of the reference files.

## 6. Proving the drift tests bite

The tests are the reason full coverage is affordable, so prove they work rather
than assuming.

```sh
# add a fake execution block to the Tool struct in internal/spec, then:
go test ./internal/skill/
```

Expected: red, naming `references/tools.md`. Revert.

```sh
# rename a page under docs-site/, then:
go test ./internal/skill/
```

Expected: red, naming the reference file whose pointer no longer resolves.
Revert.

If either one stays green, the check is decorative and the coverage is not
actually held.

## 7. The amendments

Two documents change with this feature and both are part of done:

- `.specify/memory/constitution.md`, Principle V. The command surface is no
  longer four. The amendment states that `skill` is not part of the path from
  nothing to a spoken agent and touches no package, and it bumps the version
  with a Sync Impact Report.
- `CLAUDE.md`, the "three places document a change" rule. With the documentation
  site and now the skill, it is five. A change to authoring or emitted behaviour
  updates the emitted README template, the example's own README, the page in
  `docs/`, the page in `docs-site/`, and the skill.

Check both landed:

```sh
grep -n "skill" CLAUDE.md
grep -n "Version" .specify/memory/constitution.md
```
