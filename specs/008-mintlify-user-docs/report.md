# Report: Unmute user docs on Mintlify

**Date**: 2026-08-14. **Binary**: built from `ae9f8cc`. **Site**: `docs-site/`,
not deployed.

This is the deliverable FR-020 asks for: every page written, every place where
code and an internal doc disagreed, and every claim that could not be verified.
Nothing was resolved silently.

## What shipped

| Thing | Where | Count |
|---|---|---|
| Mintlify project | `docs-site/` | 1 `docs.json`, 35 pages, the SLNG logo |
| Example READMEs written | `examples/{simple-prompt,multi-task,task-groups,subagents}/README.md` | 4 |
| Agreement tests added | `internal/cli/help_capture_test.go`, `internal/target/providers_docsite_test.go` | 2 files, 3 tests |
| Captured CLI help | `specs/008-mintlify-user-docs/help.txt` | 1 |
| Verification log | `specs/008-mintlify-user-docs/verification-log.md` | 1 |
| Contributor guide | `docs-site/README.md` | 1 |

No product Go code was changed. The only Go added is test code.

## Pages

Sidebar order. "Anchor" is the example or code the page was written from.

| # | Page | Anchor | Verified |
|---|---|---|---|
| 1 | `index` | scratch package | snippet validated, both targets |
| 2 | `start/installation` | Makefile, go.mod | commands run |
| 3 | `start/quickstart` | a real `unmute init` run | full transcript |
| 4 | `start/how-unmute-works` | ARCHITECTURE.md re-read against code | real compile output |
| 5 | `build/one-agent` | examples/simple-prompt | snippet validated |
| 6 | `build/tools` | examples/simple-prompt | snippet validated |
| 7 | `build/variables` | examples/salon-support | refusals reproduced |
| 8 | `build/two-agents` | examples/subagents | snippet validated |
| 9 | `build/tasks` | examples/multi-task | snippet validated |
| 10 | `build/task-groups` | examples/task-groups | real warning captured |
| 11 | `build/choosing-a-structure` | all four structural examples | packages cross-read |
| 12 | `dev/overview` | internal/cli/dev.go, dev_web.go | errors reproduced |
| 13 | `dev/console` | internal/cli/dev.go | read from `consolePlan` |
| 14 | `telephony/overview` | TELEPHONY.md re-verified against code | matrix cross-read |
| 15 | `telephony/first-phone-call` | examples/twilio-telephony-hello | matches example page |
| 16 | `telephony/outbound-calls` | examples/outbound-reminder | refusals reproduced |
| 17 | `telephony/webhooks-and-tunnels` | dev_tunnel.go, dev_twilio.go | refusal reproduced |
| 18 | `transfers/overview` | TRANSFERS.md re-verified | matrix cross-read |
| 19 | `transfers/livekit` | examples/livekit-human-transfer (LiveKit only) | compile report captured |
| 20 | `transfers/pipecat-daily` | examples/pipecat-human-transfer-daily (Pipecat only) | refusal captured |
| 21 | `transfers/pipecat-twilio` | examples/pipecat-human-transfer-twilio (Pipecat only) | compile report captured |
| 22 | `targets/overview` | internal/spec Target struct | struct read |
| 23 | `targets/pipecat` | a real compile | file tree quoted |
| 24 | `targets/livekit` | a real compile | file tree quoted |
| 25 | `deploy/going-live` | DEPLOYMENT.md re-verified against a build | stale claim dropped |
| 26 | `reference/cli/overview` | help.txt | agreement test |
| 27 | `reference/cli/init` | help.txt | errors reproduced |
| 28 | `reference/cli/validate` | help.txt | outputs captured |
| 29 | `reference/cli/compile` | help.txt | outputs captured |
| 30 | `reference/cli/dev` | help.txt | agreement test |
| 31 | `reference/agent-yaml` | internal/spec + internal/ir/validate.go | values read from the validator |
| 32 | `reference/targets-yaml` | internal/spec | struct read |
| 33 | `reference/providers` | internal/target/catalog_*.go | agreement test |
| 34 | `reference/variables` | internal/ir + internal/cli/dev_vars.go | messages reproduced |
| 35 | `reference/secrets` | internal/ir + a real build | messages reproduced |

Every page in `docs.json` appears above, and every page above is in
`docs.json`.

## Discrepancies

Five. None was fixed in passing; each is a maintainer decision.

### D1. `build/subagents` named a concept the code does not have

- **Code**: `grep -ri subagent internal/` returns only test names. There is no
  subagent construct in `internal/spec`.
- **Doc**: `specs/008-mintlify-user-docs/contracts/navigation.md`, the seventh
  narrative page.
- **What happened**: the example called `subagents` is two agents with handoffs,
  which `build/two-agents` teaches. The page would have restated page four under
  a new name. The slot became `build/choosing-a-structure`, a capstone comparing
  the real ways to split work. The contract was amended in place with a dated
  note. Page count unchanged at 35.
- **For maintainers**: nothing to decide unless you want the example renamed.
  The example's name is the only place the word "subagents" appears.

### D2. The root README overstates example target coverage

- **Code and packages**: `examples/livekit-human-transfer/targets.yaml` declares
  LiveKit only; `examples/pipecat-human-transfer-daily` and
  `examples/pipecat-human-transfer-twilio` declare Pipecat only.
- **Doc**: `README.md`, "Try the CLI end to end": "Every
  [example](examples/README.md) declares both `pipecat` and `livekit`".
- **Impact**: a reader who follows that sentence will try `--target livekit` on
  a Pipecat-only package and get a refusal. `examples/README.md` itself is
  correct and says which packages are single target.
- **Suggested fix**: scope the sentence to the four structural examples the
  snippet actually uses.

### D3. `go install` cannot resolve the module

- **Code**: `go.mod` declares `module github.com/slng/unmute`.
- **Repository**: `git remote -v` is `https://github.com/slng-ai/unmute_cli.git`.
- **Impact**: `go install github.com/slng/unmute@latest` cannot work, because
  the module path does not resolve to the repository. `make install` from a
  clone does work.
- **What the site says**: build from a clone, with a note that the remote install
  path does not resolve. No claim is made about which side is wrong.
- **Suggested fix**: a maintainer decision between renaming the module and
  leaving install as clone-only.

### D4. The `inject` comment and the validator disagree

- **Code A**: `internal/spec/package.go`, the `Inject` field comment: "Legal on
  webhook, local, and mcp only".
- **Code B**: `internal/ir/variables.go`, `checkInject`: mcp is rejected, with a
  reasoned message that an MCP client assembles its own arguments so an injected
  value would be dropped.
- **What the site says**: the enforced rule, webhook and local.
- **Suggested fix**: update the struct comment. The validator's reasoning is the
  one that ships.

### D5. `docs/DEPLOYMENT.md` says telephony is blocked

- **Doc**: `docs/DEPLOYMENT.md`, Checklists: "Telephony, either target: blocked
  on route promotion. Prepare against TELEPHONY.md but expect `validate` and
  `compile` to reject the route today."
- **Code and runs**: all four telephony examples validate and compile today
  (verification log section 1). `docs/TELEPHONY.md` itself says routes with a
  real adapter run and only Exotel is gated.
- **What the site says**: routes run and are marked `provisional`, with the
  compile report's own evidence lines quoted.
- **Suggested fix**: update that paragraph in DEPLOYMENT.md to match
  TELEPHONY.md.

## Unverified claims

Three, all stated in the verification log and none hidden from readers.

1. **No spoken turn was completed.** `unmute dev` ran end to end on a scaffolded
   package: compiled, image built, container healthy (Pipecat 1.5.0), dev page
   served, `/api/session` answered. The last step needs a microphone and working
   model keys and cannot be automated here. **SC-002 ("a talking agent in under
   15 minutes") is therefore unmeasured.** Everything up to speaking is verified.
2. **SLNG model ids come from this repository, not from docs.slng.ai/models.**
   That page lists display names with provider, hosting, latency, and languages,
   and does not print the id string a package writes into `model:`. The site
   prints only the two ids proven here, `slng/deepgram/nova:3-en` and
   `slng/deepgram/aura:2-en` with voice `aura-2-thalia-en`, and links the page
   for the rest of the catalog.
3. **No live phone call was made during this work.** Telephony pages present
   every route as `provisional` and quote the compile report's own evidence
   lines, including `smoke=false`.

## Rule impact: three places become four

`CLAUDE.md` says a change to emitted behaviour updates three places in the same
commit: the emitted README template, the source example's own `README.md`, and
the relevant page in `docs/`. Once this site is live there is a fourth: the
`docs-site/` page a reader actually lands on.

`docs-site/README.md` states this for contributors. **Amending `CLAUDE.md`
itself is proposed, not done**, because that file is the maintainers' rulebook.

Two of the four facts are now held by tests rather than by memory:

- `internal/cli/help_capture_test.go` fails when the CLI's help changes until
  `help.txt` is re-captured and the pages quoting it are updated.
- `internal/target/providers_docsite_test.go` fails when a catalog vendor is
  added or removed until `reference/providers.mdx` matches, and it also holds the
  "SLNG first" rule.

## Gates

| Gate | Result |
|---|---|
| `go test ./...` | pass |
| `make lint` (golangci-lint) | 0 issues |
| `gofmt -l internal/` | clean |
| `mint validate` | success |
| `mint broken-links` | no broken links |
| `mint dev --no-open` | serves, HTTP 200 |
| all ten examples validate and compile | pass |
| all ten examples have a README | pass |
| em or en dashes as punctuation | none |
| Vapi as a target | absent |
| Deepgram or ElevenLabs as a target | absent (present only as catalog model vendors) |

## Quickstart run (quickstart.md)

| Section | Result |
|---|---|
| 1. verification tooling | `make build` and `--help` pass |
| 2. every anchor ready | ten validate, ten compile, ten READMEs, `go test ./internal/generate` green |
| 3. every snippet validates | eight scratch packages, all as documented |
| 4. provider lists match the catalog | agreement test passes, and fails when a vendor row is deleted (proven, then restored) |
| 5. the site is sound | `mint validate`, `mint broken-links`, `mint dev` all pass |
| 6. the story holds | forward-reference pass done page by page; dash and target sweeps clean; the cold read of `index` is the one part a person has to do |
| 7. the report exists | this file |

## Before anyone gets a link

Deployment is deliberately outside this work. When the maintainer runs
`mint login`:

1. Create a **new** Mintlify project. Never connect it to, or push it over, the
   existing docs.slng.ai deployment. (Checked 2026-08-14: this repository had no
   `docs.json` and no `mint.json` before this feature, so the scaffold overrode
   nothing.)
2. Give it its own subdomain. Authentication works on a custom domain or a
   `*.mintlify.site` subdomain, never on a custom subpath.
3. Set visibility to **Private** in the dashboard **before sharing any URL**.
   Password authentication needs a Pro or Enterprise plan; Mintlify private
   authentication (organization members) is available on every plan and is the
   fallback.
4. Only then share the link.

---

# Addendum, 2026-08-14: feature 009-mintlify-docs-extension

**Binary**: built from merge commit `a4f46c1` (`origin/pre-release-v1` merged into
the docs work). **Site**: `docs-site/`, 49 pages, **still deployed nowhere**: no
`mint login` was run, no Mintlify project exists, no URL exists. The checklist
above is untouched and still applies.

Two causes for this work: the merge landed MCP tool sources (SCHEMA N40) and moved
the phone route into the connection file (SCHEMA N41), and the maintainer asked for
four things after reading the site (a Models section, a better Dev section, go-live
guides per platform, and a Twilio guide).

## What changed

| Thing | Where | Count |
|---|---|---|
| Pages, before and after | `docs-site/` | 35 to 49 |
| Groups | `docs.json` | 8 to 9 |
| New pages | Tools (5), Orchestration overview, Models (5), `dev/telephony`, `telephony/twilio`, two deploy guides, `reference/connections-yaml` | 16 |
| Pages moved or renamed | Build group (6), `webhooks-and-tunnels` | 7 |
| Pages retired | `build/tools`, `reference/providers` | 2 |
| Existing pages corrected for the merge | see the table below | 13 |
| Agreement tests | 3 tests before, 5 after (one new file, one retargeted) | 2 files touched |

No product Go code was changed. The only Go added or edited is test code.

## The 49 pages

Sidebar order. **New** and **moved** mark this feature's work; the rest were
re-checked and corrected where the merge made them wrong.

| # | Page | State | Anchor and verification |
|---|---|---|---|
| 1 | `index` | unchanged | scratch package, snippet still validates |
| 2 | `start/installation` | unchanged | Makefile, go.mod |
| 3 | `start/quickstart` | corrected | `unmute init` re-run: transcript unchanged; SLNG-by-design line added |
| 4 | `start/how-unmute-works` | unchanged | link re-pointed only |
| 5 | `build/your-first-agent` | moved | was `build/one-agent`; title and inbound links updated |
| 6 | `build/tools/overview` | **new** | the `Tool` struct's six blocks, held by a new agreement test; gated blocks and the one-block rule triggered |
| 7 | `build/tools/webhook` | **new** | `examples/outbound-reminder`; four refusals triggered |
| 8 | `build/tools/python` | **new** | `examples/outbound-reminder`'s handler; ruff, ty, and a run |
| 9 | `build/tools/mcp` | **new** | `examples/mcp-example`; ten refusals and one deliberate pass triggered; emission read from a real compile |
| 10 | `build/tools/prebuilt` | **new** | `internal/target/prebuilt.go`; three refusals plus the default description from a real compile |
| 11 | `build/variables` | re-checked | `internal/ir/compiler.go` variable sources unchanged |
| 12 | `build/orchestration/overview` | **new** | names the three shapes only |
| 13 | `build/orchestration/handoffs` | moved | was `build/two-agents`; reframed, snippets re-read from `examples/subagents` |
| 14 | `build/orchestration/tasks` | moved | `examples/multi-task`, re-read; delegated-scope paragraph added |
| 15 | `build/orchestration/task-groups` | moved | the LiveKit experimental warning re-run, still exact |
| 16 | `build/orchestration/choosing-a-structure` | moved | enhanced into a decision aid with a cost table |
| 17 | `dev/overview` | corrected | per-mode log files, verified by running the modes |
| 18 | `dev/console` | unchanged | `internal/cli/dev.go` console path |
| 19 | `dev/telephony` | **new** | the local run, moved out of `telephony/overview` and re-read against `dev_telephony.go`, `dev_cloud_websocket.go`, `dev_twilio.go` |
| 20 | `dev/webhooks-and-tunnels` | moved | was `telephony/webhooks-and-tunnels` |
| 21 | `telephony/overview` | corrected | N41 shape; walkthrough handed to `dev/telephony` |
| 22 | `telephony/first-phone-call` | corrected | `.env` list completed from the build's `CALL_REQUIRED_ENV`; neutral env-name example |
| 23 | `telephony/outbound-calls` | corrected | the example's two connection files; three refusals re-verified in code |
| 24 | `telephony/twilio` | **new** | the route table's environment keys, the examples' connection files, Twilio SIP trunking docs dated |
| 25 | `transfers/overview` | corrected | `destinations:` moved; the gated-transfer refusal triggered |
| 26 | `transfers/livekit` | corrected | `examples/livekit-human-transfer` re-read; compile lines re-run |
| 27 | `transfers/pipecat-daily` | corrected | the one-line connection; the dial-out prerequisite re-captured (it changed) |
| 28 | `transfers/pipecat-twilio` | corrected | full-route connection; compile lines re-run; warm refusal added |
| 29 | `targets/overview` | corrected | target field list; providers link re-pointed |
| 30 | `targets/pipecat` | unchanged | a real Pipecat build |
| 31 | `targets/livekit` | unchanged | a real LiveKit build |
| 32 | `models/stt` | **new** | `catalog_*.go`, held by the retargeted agreement test |
| 33 | `models/tts` | **new** | same |
| 34 | `models/llm` | **new** | same |
| 35 | `models/turn-detection` | **new** | the emitted code on both targets; held by a test asserting the `turn` role has no catalogue vendors |
| 36 | `models/optimization` | **new** | SLNG's own docs, quoted, linked, and dated 2026-08-14 |
| 37 | `deploy/livekit-cloud` | **new** | the emitted runbook, cross-read against LiveKit's docs 2026-08-14 |
| 38 | `deploy/pipecat-cloud` | **new** | the emitted runbook plus `pcc-deploy.toml`, cross-read against Pipecat Cloud's docs 2026-08-14 |
| 39 | `deploy/going-live` | corrected | neutral env-name example with the real refusal; new cards |
| 40 | `reference/cli/overview` | unchanged | binary help, held by the help test |
| 41 | `reference/cli/init` | corrected | the interactive scaffold's connection file |
| 42 | `reference/cli/validate` | unchanged | binary help |
| 43 | `reference/cli/compile` | corrected | file list re-captured under `--target pipecat` |
| 44 | `reference/cli/dev` | unchanged | binary help |
| 45 | `reference/agent-yaml` | corrected | `destinations:`, the `secrets:` rule, the `mcp:` block's four fields |
| 46 | `reference/targets-yaml` | corrected | shrunk to seven fields; three moved-field refusals |
| 47 | `reference/connections-yaml` | **new** | three shapes, each from an example or a validated scratch package |
| 48 | `reference/variables` | unchanged | re-read against `internal/ir` |
| 49 | `reference/secrets` | corrected | the author-wrote rule, both lists, the two-list `.env.example` |

`find docs-site -name "*.mdx" | wc -l` is 49 and the `docs.json` page count is 49.

## D1 to D5, re-checked on the merged tree

| # | Verdict | Evidence on the merged tree |
|---|---|---|
| D1. `build/subagents` named a concept the code lacks | **stands** | `grep -ri subagent internal/` still returns only test names and the example's own directory name. Nothing to decide unless the example is renamed. |
| D2. root README overstates example target coverage | **stands, and is now further off** | `README.md:81` still says "Every [example] declares both `pipecat` and `livekit`". There are eleven examples now; the three transfer packages are still single target. `examples/README.md` is still correct. |
| D3. `go install` cannot resolve the module | **stands** | `go.mod` is `module github.com/slng/unmute`; `origin` is `https://github.com/slng-ai/unmute_cli.git`. |
| D4. the `Inject` comment and the validator disagree | **stale, the merge fixed it** | `internal/spec/package.go:216-219` now reads "Legal on webhook and local only", which is what `internal/ir/variables.go` `checkInject` enforces. N40 resolved it. No action left. |
| D5. `docs/DEPLOYMENT.md` says telephony is blocked | **stands** | `docs/DEPLOYMENT.md:247` still says "blocked on route promotion ... expect `validate` and `compile` to reject the route today", while all five telephony examples validate and compile. |

## New findings (D6 to D8)

None was fixed in passing. Each is a maintainer decision.

### D6. The emitted LiveKit runbook still teaches a provider-branded bad name

- **Template**: `internal/generate/templates/livekit_v1/README.md.tmpl`, so every
  LiveKit build's README (seven of them in `examples/*/build/livekit/`) uses
  `11LABS_API_KEY` as the example of an unexportable name and tells the reader to
  rename it to `ELEVENLABS_API_KEY`.
- **Site**: the same teaching now uses a neutral `2FACTOR_*` name, because a
  branded one reads as a statement about a vendor (maintainer instruction,
  2026-08-14).
- **Also affected**: `examples/twilio-telephony-hello/README.md:118` and
  `examples/livekit-human-transfer/README.md:136` and `:240`, plus
  `specs/008-simplify-telephony-connections/contracts/environment.md:60`, which
  names that example's use of the branded name as a deliberate teaching line the
  documentation check must not break.
- **Suggested fix**: swap the name in the template and the example README if the
  maintainers want the rule applied everywhere, not only on the site. Doing it here
  would have been a product-doc change made in passing.

### D7. Pipecat Cloud's own docs disagree about `secrets set --file`

- **The emitted runbook** tells the reader
  `pipecat cloud secrets set <set> --file .env --region <region>`.
- **Pipecat Cloud's secrets reference page** documents only the key-value form
  (`secrets set my-secrets KEY value`), read 2026-08-14. Its **guides** (Datadog,
  WhatsApp) use `--file .env`, read the same day via context7.
- **Impact on us**: none, the flag is real. Recorded because our runbook depends on
  a form the platform's reference page does not list, so a future platform change
  would surface there first.

### D8. One `.env`, two routes, and a startup check that asks for both

- `examples/twilio-telephony-hello` declares both targets, so `agent.yaml`'s
  `secrets:` lists both credential groups. The Pipecat build's
  `CALL_REQUIRED_ENV` therefore demands the four `SIP_*` names its own route never
  reads, and a phone call on that target stops without them.
- This is the accepted consequence of the N41 rule that `secrets:` lists every name
  the author wrote (`contracts/environment.md`), not a defect.
- **What the site does**: `telephony/first-phone-call` lists all the names and says
  why, rather than shipping a `.env` that fails on the first call.
- **For maintainers**: nothing to fix unless per-route filtering of the call-time
  check is wanted later.

### Checked and **not** a discrepancy

`kind:` in a connection file. The `Kind` field is still on
`internal/spec.Connection` (`package.go:356`) although the contract says nobody
writes it. That is deliberate: strict decode has to accept the key so
`internal/ir/build.go:97` can refuse it with the file, the line, and the fix rather
than a bare unknown-key error. Triggered and confirmed. Code and contract agree.

## One page has no agreement test, on purpose

`reference/connections-yaml` states the three valid connection shapes and the
per-route environment keys. Those facts live in `internal/target/telephony.go` and
could be held by a test, but they are not: the page's shapes are backed by two
shipped examples and one validated scratch package, and every telephony example is
validated and compiled by the default suite. This is the same precedent 008 set for
pages whose facts a few examples already cover, and it is recorded here rather than
left implicit. If the route table grows a new environment key, the example that uses
it is what fails first.

## Unverified claims

The three from the original report still stand: no spoken turn was completed, SLNG
model ids come from this repository rather than from docs.slng.ai/models, and no
live phone call was made. Added by this work:

1. **No cloud deploy was executed.** Every command on `deploy/livekit-cloud` and
   `deploy/pipecat-cloud` comes from the runbooks this repository generates and was
   cross-read against each platform's current documentation on 2026-08-14. None was
   run: that needs a real account and a billable agent. Both pages say so in their
   own words.
2. **The ready-state telephony output is quoted from format strings, not from a
   run.** The managed-tunnel line, the webhook set and restore lines, and the call
   line on `dev/telephony` are the code's own `fmt.Fprintf` strings with placeholder
   values. The run that would print them changes a real number's webhook, which this
   work did not do. What was run: the same mode with `--no-webhook`, far enough to
   create `telephony.log` and print its path.
3. **The execution-layer figures are SLNG's, for the whole layer.** 39% latency and
   53% cost are published for the Execution Layer as a whole, which upstream
   describes as three stages; this site documents two of them by maintainer
   decision. Nothing here measures either figure, and `models/optimization` states
   that in the same breath as the numbers.
4. **Twilio's console path for the Account SID and auth token is attributed, not
   sourced.** The Twilio pages read on 2026-08-14 name both values without giving a
   console location; the "account dashboard" wording comes from this repository's
   generated runbooks.

## Gates, this addendum

| Gate | Result |
|---|---|
| `go test ./...` | pass, including the new and retargeted agreement tests |
| `make lint` | 0 issues |
| `gofmt -l internal/` | clean |
| `mint validate` | success |
| `mint broken-links` | no broken links |
| all eleven examples validate and compile with a README | pass |
| `.mdx` count equals `docs.json` page count | 49 = 49 |
| em or en dashes as punctuation | none |
| Vapi as a target | absent |
| `11LABS` anywhere in `docs-site/` | absent |
| Context Router mentioned anywhere in `docs-site/` | absent |
| docs site deployed | **no**: no `mint login`, no project, no URL |
