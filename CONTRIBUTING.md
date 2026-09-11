# Contributing to Unmute

Contributions are welcome. Unmute is MIT licensed and open all the way
through: the compiler, the three targets, the examples, the coding agent skill
and the docs site. You do not need permission to open a pull request, and you
do not need to work at SLNG.

If you want to talk something through first, we are on
[Discord](https://discord.gg/kxZactmWj).

## What a pull request needs

Five things. A pull request missing one gets sent back for it, which is a
slower round trip than adding it now.

| | What |
|---|---|
| 1 | **An issue**, opened before you write the code |
| 2 | **An example package that uses your new feature** |
| 3 | **A video** of that example working |
| 4 | **A README** for that example |
| 5 | **Updated docs**, explaining how the feature works |

Two and three are there because you cannot judge a voice agent from a diff. A
diff shows that a field parses. It does not show whether the agent still waits
for the caller to finish. We need to run your feature and hear it.

Each one is explained below, then how to send the change.

## 1. Open an issue first

**Check whether it already exists.** Search the
[open issues](https://github.com/slng-ai/unmute/issues) before filing. If it is
already there, add what you know to that thread: your version, your target,
your route, your logs. A second copy splits the conversation.

**If it is not there, open one.** There are three templates. Blank issues are
off, because each template asks for the thing that makes that kind of report
actionable.

| Template | Use it when |
|---|---|
| [Bug report](https://github.com/slng-ai/unmute/issues/new?template=1-bug.yml) | Something is broken: a wrong refusal, a bad compile, an agent that misbehaves on a call |
| [Improvement](https://github.com/slng-ai/unmute/issues/new?template=2-improvement.yml) | It works, but not well enough: latency, cost, audio, a rough error message |
| [Feature request](https://github.com/slng-ai/unmute/issues/new?template=3-feature-request.yml) | It does not exist at all: a target, a provider, a transport, a tool kind, a call behaviour |

**Open it before you write the feature.** Unmute compiles one authored package
into whatever each target framework needs, so a new field is rarely a local
decision. It has to mean something on Pipecat, on LiveKit Agents and on SLNG,
or be refused by name where it cannot. Settling that on an issue takes a day.
Finding it out on a finished branch costs you the branch.

**Link the issue from the pull request.** Put `Closes #123` in the body.

## 2. Add an example package that uses your feature

Every change to what Unmute emits, accepts or refuses ships with a package
that **uses the thing you added**. Not a snippet in the pull request body: a
package on disk, in the same branch, that validates and compiles.

```sh
unmute validate examples/your-package
unmute compile examples/your-package
```

### The example has to exercise the feature

This is the part people get wrong. A package that compiles but never touches
your new field proves nothing, and we cannot review it. So:

- **Your new key appears in the authored files.** If you added a `retry:` field,
  a package in the branch writes `retry:` in its `agent.yaml` or its tool file.
- **It runs on a call.** Drive the agent to the point where your code path
  actually executes. If the feature only fires on a phone route, the package
  declares that route.
- **It shows the edge you care about.** If the feature has a timeout, a skip or
  a refusal, the package can reach it. Pick values somebody can hit while
  talking.
- **It compiles on every target the feature claims.** If your field works on
  both code targets, the package declares both.

If you can extend an existing example instead of adding one, do that. A new
public example is a bigger commitment than a new field on an old one.

If your change cannot be shown by a package somebody can run, say so on the
issue before you build it. That usually means the change wants a different
shape.

### Where the package goes

| Directory | What lives there |
|---|---|
| [`examples/`](examples/) | Packages a user is sent to read. Public, and held to every gate below. |
| `internal/voice-agents-tests/` | Whole agents we compile, deploy and call against real providers. Not shipped, not reader facing. One bar: it validates and generates on every target it declares. |
| `internal/testdata/` | The smallest package that makes one unit assertion possible. No prose, no README. |

A package meant to teach belongs in `examples/`. A package that exists to be
dialled rather than read belongs in `internal/voice-agents-tests/`. That split
keeps the public set from growing with work nobody outside is meant to read.

### What a package contains

Copy the shape from [`examples/customer-intake`](examples/customer-intake/),
which is the small one:

```
agent.yaml         # who the agent is, its models, its tools
targets.yaml       # which targets it compiles to
instructions.md    # the entry agent's prompt
README.md          # what it does and what to listen for
tasks/             # one Markdown prompt per task
tools/             # one YAML file per tool, plus any local Python handler
connections/       # a phone route, if the package has one
```

`build/` is generated. It is git ignored and never committed.

### What a public example has to pass

A new directory under `examples/` joins the public set on purpose. The suite
fails until its name is written into the hardcoded list in
`internal/generate/examples_test.go`. Then these apply, and `make test` runs
all of them:

- It loads, builds, validates with zero errors and generates on every target it
  declares.
- `agent.yaml` is block style. No inline `{` or `[` outside comments.
- No specimen phone number and no specimen email address in any prompt. A model
  cannot tell an illustration from a value it is holding, so it reads it out. A
  live call once opened with the example number from its own formatting rule,
  and the caller agreed to a number that was not theirs. Describe the grouping
  in words.
- The README names every transport the package declares.
- Every relative link in the package's Markdown resolves on disk.
- Every `destinations:` value is an environment variable name, never a literal
  phone number or SIP URI.
- Model ids and framework versions match what the rest of the tree pins.
- The emitted Python passes `ruff check`.

Each of these fails with the file, the line and what to write instead.

## 3. Attach a video

Record your screen, with sound, while you talk to the agent. `unmute dev` opens
the browser loop. Use a real phone call if the change is about a phone route.
Drag the file into the pull request body.

Keep it short and answer four things:

- Which target and which route.
- Where the feature fires. Drive the agent to that moment.
- What to listen for: the pause, the interruption, the value the agent did not
  have to ask for.
- What it used to do, if there is a before worth hearing.

A minute or two is enough. Skip or blur anything holding a real key, a real
phone number or real customer data.

If the change has no audible surface, record the terminal instead: the
refusal, the error, or the generated file. Say which it is.

## 4. Write the README

One per package. It is what somebody reads before running anything. Say:

- What the agent does, in two or three sentences.
- Which targets it declares, and which transports.
- What it needs before it runs: environment variables, an account, a phone
  number, Docker or uv.
- How to run it, as commands somebody can paste.
- **Which part of it is your feature**, and what to listen for so a reader
  knows whether it worked.

The READMEs under `examples/` are the model. Copy the one closest to what you
built.

## 5. Update the docs

A feature nobody can find is a feature nobody uses. If your change adds,
removes or alters behaviour a user can see, the docs move in the same pull
request.

### Where to write it

| Surface | When it changes |
|---|---|
| `docs-site/` | Always, for a user-visible change. This is the public answer somebody lands on. |
| The page's key list under `docs-site/reference/` | You added or changed a key in `agent.yaml`, `targets.yaml` or a connection file. |
| The example's own `README.md` | Covered in step 4. |
| The emitted runbook template in `internal/generate/` | You changed what `build/<target>/README.md` should say. |
| `internal/skill/assets/` | Always, for a new authoring surface. This is what a coding assistant reads before it writes a package. |

Those last four are the four surfaces rule: a change to emitted behaviour
updates the runbook template, the example README, the docs page and the skill,
in the same commit. A fact that is only true in generated output is a fact no
reader ever sees, and a feature the skill does not know about is a feature no
coding agent will use.

### Explain the logic, not just the key

A docs page that lists a field name and its type has not explained anything.
Write what somebody needs to predict the behaviour:

- **What it does**, in one sentence a reader can repeat back.
- **When to reach for it**, and when not to. Name the problem it solves.
- **What it compiles to on each target.** Say plainly where the targets differ,
  and say when a target refuses it.
- **What happens if you leave it out.** The default, and why that default.
- **What happens when it goes wrong**: the timeout, the skip, the refusal, and
  what the caller hears while it happens.
- **A worked snippet** a reader can paste, taken from the example you added in
  step 2 so the page and the package cannot drift.

The docs site has its own rules in
[`docs-site/README.md`](docs-site/README.md). Two catch people out: no page
states a version, and no page uses an em or en dash as punctuation. Both are
tests, so you will hear about it from `make test` rather than from review.

## How to send the change

1. **Fork** the repository and clone your fork.
2. **Branch** from `main`.
3. **Make the change**, with the example, the README and the docs beside it.
4. **Run the checks** below until they are clean.
5. **Commit** in plain words. Say what the change does, the way you would say
   it out loud.
6. **Push** to your fork and open a pull request against `main`.

One pull request, one change. Two unrelated fixes in a branch means neither can
merge until both are agreed.

### Run the checks before you push

CI runs six jobs on every pull request. Each has a local equivalent, and
running them locally beats waiting for the runner.

| CI job | Run it locally |
|---|---|
| `format` | `make fmt`, then `go mod tidy` and check `go.mod` and `go.sum` are unchanged |
| `test` | `make test`, which is `go test -race ./...` with no Python, no network and no accounts |
| `lint` | `make lint` |
| `python` | `ruff check .`, over every checked-in `.py` including an example's tool handlers |
| `vuln` | `govulncheck ./...` |
| `release-config` | `make release-dry` |

Two suites are opt-in and never the pull request gate:

- `make smoke` proves the emitted Python actually runs. It needs Python and the
  provider SDKs installed.
- `make contracts` re-fetches the published SLNG conformance fixtures. It needs
  network.

Run `make smoke` yourself if you changed what gets emitted. It catches a
template that produces Python which does not run.

### A rule with no gate is a wish

Standards here are things a test fails on, not things a reviewer has to
remember. If your change adds a rule, wire its check in the same pull request.
If a gate you did not expect starts failing, fix the code rather than the gate.

### The changelog writes itself

Nobody types a changelog entry. The changelog page is rendered from the GitHub
Release after a tag is published. Do not add a file for it, and do not edit
`docs-site/changelog.mdx` by hand.

## Pull request body

Two parts:

- **What's in.** The shape an author writes, in a fence, and one paragraph on
  what it does. What was wrong before, if it takes a line.
- **How it works.** Five to ten lines, covering the decisions somebody would
  otherwise ask about.

Then the issue it closes, the example it added, and the video. Longer
reasoning belongs in a code comment beside the thing it explains, or in the
commit message, where a reader finds it later.

## What happens next

A maintainer reads the issue, runs your example, watches the video, then reads
the diff. Expect questions about the call rather than about the code.

If something needs changing it gets said on the pull request. Ask on the issue
or on Discord if you are not sure how to answer it.

## Reporting a security problem

Do not open a public issue for a vulnerability. Use GitHub's private
vulnerability reporting on the Security tab, or reach a maintainer on
[Discord](https://discord.gg/kxZactmWj), and give us time to ship a fix before
you write about it.

## How we treat each other

Assume the other person is trying to help. Review the change, not the person
who wrote it. Keep threads about the work. Maintainers will edit or remove
abusive comments and close threads that stop being about the code.

## License

Unmute is MIT licensed. By contributing you agree your contribution is licensed
under the same terms. See [LICENSE](LICENSE).
