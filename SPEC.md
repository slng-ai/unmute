# SPEC — one YAML style everywhere (PR #67 onto the integration branch)

Source brief: land [PR #67](https://github.com/slng-ai/unmute_cli/pull/67)
("one YAML style everywhere, pin emitted ruff, add CI") onto
`mergesim-unmute-updates`, and prove the restyle is complete across **every**
YAML surface — not tools alone: `agent.yaml`, `targets.yaml`, `tools/*.yaml`,
`connections/*.yaml`, task and agent files, `internal/testdata` fixtures, the
`unmute init` scaffold templates, and every `yaml` fence under `docs/`.

The shape wanted, without exception:

```yaml
# before                                # after
tools: [check_customer, manage_appt]    tools:
                                          - check_customer
                                          - manage_appt
interruption: { enabled: true }         interruption:
                                          enabled: true
```

**PR #67 is based on `main` and predates the execution-keyed tool block (N19)
that reached this branch with the webhook/auth work.** It restyles the *old*
flat tool shape (`execution: webhook` + top-level `url_env:`). The merge is
therefore not a fast-forward: four files conflict and two of them need both
sides, not one. Measured on this branch, not assumed.

Baseline on `mergesim-unmute-updates` before the merge: **37 authored YAML files
carry flow style; 137 flow-style lines sit in `docs/` yaml fences.**

## §G goal

Every YAML unmute authors, scaffolds, ships as a fixture, or prints in docs is
block style, the tool files keep their execution-keyed shape, and nothing about
the compiled output changes.

## §C constraints

- C1: block style applied **without exception**, including trivial single-key
  maps (`{ type: string }`) and short sequences (`required: [a, b]`). "Block
  unless short" is two styles, not one, and recreates the ambiguity being
  removed.
- C2: the merge resolves toward **this branch** on tool shape. N19 (execution
  kind is a block name: `webhook:`, `local:`, `mcp:`, `builtin:`, `client: {}`,
  `provider_hosted: {}`) wins over PR #67's flat `execution:`/`url_env:`/
  `handler:` keys everywhere the two disagree. PR #67's restyle, its restored
  `check_availability` description, and its N21/N22 notes all survive.
- C3: `client: {}` and `provider_hosted: {}` keep the empty flow mapping. It is
  the only spelling of "this block is intentionally empty" — block style has no
  empty form — and N19 fixes it as the shape. C1 is about non-empty collections.
- C4: **semantics unchanged.** Every restyled document parses to a structure
  identical to its pre-restyle self. Restyle is not an edit.
- C5: **compiled output unchanged.** Every generated file across all five
  examples and both drivers stays byte-identical, compared with `ruff` off
  `PATH` so raw generator output is compared and not formatted output.
- C6: emitted `docker-compose` goldens are **out of scope** and keep exec-form
  arrays (`command: ["python", "bot.py"]`, healthcheck `test:`). That is the
  Docker idiom, the files are generated infrastructure rather than an unmute
  authoring surface, and converting them churns four goldens for no reader.
  Flagged for the user rather than decided silently — see T9.
- C7: no new Go or Python dependency. `goccy/go-yaml` is already direct.
- C8: the four PR follow-ups are **recorded, not fixed** here. Each is either
  pre-existing on `main` or needs a design call. None blocks the merge; see
  §T T10–T13 and the note under §B.
- C9: L1–L3 stay zero-Python. Anything needing `uv`, Docker or network is L4
  (`make smoke`), opt-in, never the PR gate.

## §I surfaces

- `I.yaml.authored` — `examples/*/{agent,targets}.yaml`,
  `examples/*/tools/*.yaml`, `examples/*/connections/*.yaml`,
  `internal/testdata/{remy,safe_core}/**`.
- `I.yaml.scaffold` — `internal/scaffold/templates/{agent,targets,tool}.yaml.tmpl`
  and the `blockYAML` template function (`yamlBlock`) in
  `internal/scaffold/scaffold.go`.
- `I.yaml.docs` — every ```` ```yaml ```` fence under `docs/`, including
  `docs/SCHEMA.md`, `docs/user/**`, `docs/ORCHESTRATOR_SHARED_CONFIGURATION.md`.
- `I.cli.compile` — `internal/cli/compile.go` `formatPython`: ruff invocation and
  parse-failure classification.
- `I.ir.Validate` — `internal/ir/validate.go` tool/task schema-key walk (N21).
- `I.ci` — `.github/workflows/ci.yml`.

## §V invariants

- V1: no flow mapping or flow sequence in any file under `I.yaml.authored` or
  `I.yaml.scaffold`. Exception: the C3 empty mappings.
- V2: no flow mapping or flow sequence inside any `yaml`-tagged fence under
  `docs/`, including inside YAML comments — a comment that spells
  `{enum: [a, b]}` teaches the style the house rule just banned.
- V3: for every restyled document, `parse(before) == parse(after)` structurally.
- V4 (amended 2026-08-10 during T8): every generated file for all five examples
  on both drivers is byte-identical before and after, with `ruff` absent from
  `PATH`, **except `pyproject.toml`**, which changes by design under V8. As
  first written V4 said "every generated file" and so contradicted V8; measured,
  the exception is exactly the ruff pin and its comment in 10 of 151 files.
- V5: a scaffolded package contains zero flow style for **every** execution kind,
  including the `webhook:` + `auth:` block that N19 added and PR #67 never saw.
- V6: `yamlBlock` decodes as YAML, never `encoding/json`. `speed: 1` stays the
  integer `1` and never re-emits as `1.0`; params are forwarded verbatim under
  D10, so widening a number is a behaviour change, not cosmetics.
- V7: tool files keep the N19 execution-keyed block after the merge. No
  top-level `execution:`, `url_env:`, or `handler:` key returns anywhere,
  fixtures and scaffold template included.
- V8: both `pyproject.toml` templates pin `ruff==0.15.7`, so an emitted project
  passes the `ruff check .` gate its own README promises (driver-livekit V26).
- V9: `compile` fails **only** on ruff's `error: Failed to parse` marker, matched
  after ANSI stripping, with `--isolated --color never`. Any other ruff failure
  warns, keeps the unformatted source, and exits 0 — exit code 2 alone cannot
  tell a generator defect from a stray `ruff.toml` above the working directory.
- V10: schema-key findings (N21) warn on stderr with the exact path and exit 0.
  They never fail a build: JSON Schema requires unknown keywords be ignored, so
  the offending map is itself a valid schema.
- V11: `make test` and `make lint` are green on the merge result.

## §T tasks

id|st|task|cites
-|-|-|-
T1|x|merge `chore/yaml-style-smoke-parity` (881d645) into `mergesim-unmute-updates`, no fast-forward|C2
T2|x|resolve `internal/testdata/remy/tools/check_availability.yaml` + `internal/testdata/safe_core/tools/lookup_customer.yaml` — take this branch's `webhook:` block, keep PR's block-style `input`/`output` and the restored `"The requested date, e.g. 2026-08-14"` description|C2,V7
T3|x|resolve `internal/scaffold/templates/tool.yaml.tmpl` — **both sides**: this branch's execution-keyed block + `auth:` block, PR's `yamlBlock 2 .Input` / `yamlBlock 2 .Output`. Neither side alone is correct: ours still interpolates flow JSON, theirs still emits flat `execution:`|C2,V5,V7
T4|x|resolve `docs/SCHEMA.md` — **both sides**: keep N19 (execution block) and its field-table rows, add PR's N21 (schema-key reporting) and N22 (`output` enforced nowhere), and take PR's corrected `output` row wording|C2,V10
T5|x|restyle any authored YAML the merge leaves in flow style; none remained, but two fixture comments still claimed `output` was "enforced on code targets" against N22 and were corrected|V1,V3
T6|x|restyle the two flow-style lines left in `docs/ORCHESTRATOR_SHARED_CONFIGURATION.md` yaml fences (`:227` `# enums spell as {enum: [a, b]}`, `:277` `# requires: [verified]`) — both inside comments, both still teaching the banned style|V2
T7|x|convert `.github/workflows/ci.yml:11` `branches: [main]` to block style — the file PR #67 adds breaks the rule PR #67 establishes|V1
T8|x|verify: V1–V6 and V11 hold. Flow scan zero; 96/96 docs fences structurally identical; 41 package files with exactly one intended semantic change (the restored description); 151 generated files identical but the intended ruff pin; `make test` and `make lint` green|V1,V2,V3,V4,V5,V6,V11
T14|x|`yamlBlock` indents sequences (`yaml.IndentSequence(true)`) — goccy's default put the dash in the parent key's column, making a scaffolded file the one place whose sequences differed from every hand-authored package|V1,V5
T15|x|cover V5/V6 with `TestScaffoldToolManifestsAreBlockStyle` — all seven execution kinds incl. `webhook` + `auth`, asserting block style, indented sequences, and no int-to-float widening. No test covered a scaffolded tool file's style before|V5,V6
T9|.|ask the user whether emitted `docker-compose` goldens should also convert (C6 defaults to no)|C6
T10|.|**gap 1, record only** — emitted Pipecat task code fails `ty`: `self.context` `Unknown \| None` at `bot.py.tmpl:240`, `self.state` at `:283`. Pre-existing; `ty` coverage not widened into a red gate|C8
T11|.|**gap 2, record only** — `bot.LLMWorker` referenced by the tracing smoke script is not emitted when tracing and function calls are both on (`bot.py.tmpl:59`). Pre-existing on `main`, L4 only|C8,C9
T12|.|**gap 3, record only** — tool `output` enforced nowhere (N22). Needs a design call: what a generated agent does when a tool returns a mismatched value mid-conversation, after the call already happened|C8
T13|.|**gap 4, record only** — ruff `B010` ("replace `setattr` with assignment") contradicts driver-pipecat V24, which locks dynamic-safe `setattr`. Dormant while ruff is pinned at 0.15.7; live again on any unpin|C8,V8

## §B bugs

id|date|cause|fix
-|-|-|-

None yet for this feature. The four PR follow-ups (T10–T13) are **not** §B rows:
three are pre-existing conditions rather than regressions this work caused, and
the fourth is an unresolved design question. They become §B rows if one of them
turns out to break something this branch ships.
