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
