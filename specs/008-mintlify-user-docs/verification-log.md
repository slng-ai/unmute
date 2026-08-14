# Verification log

Everything the docs site claims was checked here first. Commands were run in
this worktree against the binary built from commit `ae9f8cc` on 2026-08-14.

## 1. Baseline: all ten examples (T005)

`./bin/unmute validate <example>` and `./bin/unmute compile <example>`, run on
every package in `examples/`. All ten validate and compile.

| Example | Declared targets | Validate | Compile | Notes printed |
|---|---|---|---|---|
| simple-prompt | livekit, pipecat | pass | pass | livekit: "LiveKit turn placement is a preference" |
| salon-support | pipecat, livekit | pass | pass | same warning |
| multi-task | livekit, pipecat | pass | pass | same warning |
| task-groups | livekit, pipecat | pass | pass | same warning plus "LiveKit TaskGroup is experimental" |
| subagents | livekit, pipecat | pass | pass | same warning |
| twilio-telephony-hello | livekit (sip), pipecat (cloud-websocket) | pass | pass | route, required env, and provisional evidence lines |
| outbound-reminder | livekit (connector), pipecat (carrier-websocket) | pass | pass | route, endpoints, required env, provisional evidence |
| livekit-human-transfer | livekit only (sip) | pass | pass | cold and warm transfer evidence, both provisional |
| pipecat-human-transfer-daily | pipecat only (daily-sip) | pass | pass | validate prints a `daily_dialout` setup prerequisite |
| pipecat-human-transfer-twilio | pipecat only (cloud-websocket) | pass | pass | cold transfer evidence, provisional |

Facts taken from these runs and used on the site:

- Warnings print to stderr and the command still exits 0. Verified by reading
  `internal/cli/validate.go` and by the runs above.
- Sizing lines carry `[unbenchmarked]` and a date, for example
  `sizing workers=10 [unbenchmarked] (2026-07-15 conservative 1 session per worker/GPU; channels=realtime_audio)`.
- Model bindings print `(forwarded as-is, not validated)`.
- Telephony evidence lines are `provisional` with a vendor doc URL and a
  verification date, and `smoke=false`. No route on the site is presented as
  more proven than that.
- `examples/*/build/` is gitignored, so compiling locally leaves the tree clean.

## 2. Anchors confirmed against the examples' YAML (T006)

The page inventory in `contracts/navigation.md` marked three anchors
"(confirm)". Reading the packages settled them, and one contract change came
out of it.

| Page | Anchor decided | Why |
|---|---|---|
| build/tools | `examples/simple-prompt` | It declares five tools and no variables, so a tools page written from it cannot forward-reference variables or `inject:`. |
| build/variables | `examples/salon-support` | It is the only package with a full `variables:` block covering both sources (`call_start` and `conversation`), plus `inject:` and the generated `update_variables` tool. `outbound-reminder` also uses variables but adds telephony, which the narrative has not taught yet. |
| build/two-agents | `examples/subagents` | `examples/salon-support` has exactly one agent, so it cannot anchor a two-agents page. `examples/subagents/agent.yaml` declares two agents (`booking_desk`, `appointment_manager`) and two `kind: agent_transfer` controls. |

**Contract change (discrepancy D1, carried into the report).** The planned
seventh narrative page, `build/subagents`, has no concept behind it. There is
no subagent construct in `internal/spec`: `grep -ri subagent internal/` returns
only test names and the example directory. The example called `subagents` is
"two agents with handoffs" (its own header comment, and the table in
`examples/README.md`), which is exactly what `build/two-agents` teaches. The
page would have restated page four under a new name. It was replaced with
`build/choosing-a-structure`, which compares the three real ways to split work
(more tools, a task, a second agent) and is the natural capstone. Page count is
unchanged at 35.

## 3. Provider catalog (T048 source data)

Read out of `internal/target/catalog_pipecat.go` and `catalog_livekit.go` by
calling `Catalog.Vendors` for each framework and role. Roles in the catalog are
`listen`, `speak`, `reason`, `turn`.

- pipecat listen: assemblyai, cartesia, deepgram, elevenlabs, gradium, openai, slng, soniox, speechmatics
- pipecat speak: cartesia, deepgram, elevenlabs, gradium, inworld, openai, rime, sarvam, slng, soniox
- pipecat reason: anthropic, deepseek, google, groq, mistral, openai, openrouter, qwen
- livekit listen: assemblyai, cartesia, deepgram, elevenlabs, gradium, sarvam, slng, soniox, speechmatics
- livekit speak: cartesia, deepgram, elevenlabs, gemini, gradium, inworld, rime, sarvam, slng, soniox
- livekit reason: anthropic, aws, azure, groq, mistralai, openai, openrouter, sarvam
- turn: no catalog entries on either framework. Turn detection is not a catalog
  vendor slot; it is a `models.turn` binding forwarded to the runtime
  (`provider: local, model: silero` on Pipecat, `provider: livekit,
  model: turn-detector-mini` on LiveKit in every shipped example).

Wildcard rows exist for pipecat listen, speak and reason, and for livekit
reason. They require `endpoint_env`, which is why an unlisted vendor is legal
only as an OpenAI-compatible endpoint. LiveKit listen and speak have no
wildcard row, so those two lists are closed.

## 4. SLNG model names

`https://docs.slng.ai/models` lists model display names with provider, hosting,
latency and language columns. It does not print the id string a package writes
into `model:`. The site therefore shows only the ids proven in this repo and
links the page for the catalogue:

- `slng/deepgram/nova:3-en` (listen)
- `slng/deepgram/aura:2-en` with voice `aura-2-thalia-en` (speak)

Both appear in every shipped example, in `internal/scaffold/scaffold.go` as the
`unmute init` defaults, and in the compile report lines above. Recorded as a
partial verification: the ids are verified against this repo, not against the
SLNG models page.

## 5. Snippets run through validate

Full-package YAML blocks were assembled in scratch packages and run through
`./bin/unmute validate`. Each one passed for both declared targets, with the
usual LiveKit turn placement warning.

| Scratch package | Covers | Result |
|---|---|---|
| `snip/minimal` | the `index` page's agent.yaml plus a two-target targets.yaml with a LiveKit turn override | pass (livekit, pipecat) |
| `snip/index2` | the `index` page's targets.yaml exactly as printed, with no override | pass (livekit, pipecat) |
| `snip/one-agent` | `build/one-agent` agent.yaml and targets.yaml | pass |
| `snip/tools-page` | `build/tools`: a local tool file, a builtin tool file, both tool lists | pass |
| `snip/two-agents` | `build/two-agents`: two agents and two `agent_transfer` controls | pass |
| `snip/tasks-page` | `build/tasks`: a task with a typed `result`, a `delegate` control with `assign`, and the two variables it assigns into | pass |
| `snip/secret-template` | the secrets-in-template refusal, by editing a copy of salon-support | fails as documented, exit 1 |
| `snip/badprovider` | the unlisted-provider refusal on Pipecat, and its LiveKit Inference fallback | fails on pipecat as documented, exit 1 |

Snippets on the variables, task-groups, telephony, and transfer pages are
excerpts of shipped examples (`salon-support`, `task-groups`,
`twilio-telephony-hello`, `outbound-reminder`, the three transfer packages),
each of which validates and compiles as a whole (section 1).

Command output quoted on the site was captured by running the command, not
retyped. The captured runs include: `unmute init my-agent`, `unmute validate`
and `unmute compile` on the scaffold and on the examples, `unmute dev` on the
scaffold (default ports and custom ports), the `--var` refusal, the
`--to`/`--public-url`/`--target` refusals, the Daily route refusal, and
`unmute --version`.

## 6. Discrepancies found

- **D1** (resolved by contract change, section 2): `build/subagents` named a
  concept the code does not have.
- **D2**: `README.md` at the repository root says "Every
  [example](examples/README.md) declares both `pipecat` and `livekit`". Three
  examples declare one target only. Reported, not fixed here.
- **D3**: the Go module path is `github.com/slng/unmute` while the repository is
  `github.com/slng-ai/unmute_cli`, so `go install github.com/slng/unmute@latest`
  cannot resolve. The installation page documents building from a clone and says
  why. Reported, not fixed here.
- **D4**: the `Inject` field comment in `internal/spec/package.go` says inject is
  "legal on webhook, local, and mcp only", while `internal/ir/variables.go`
  `checkInject` rejects mcp with a reasoned message. The site documents the
  enforced rule (webhook and local). Reported, not fixed here.
- **D5**: `docs/DEPLOYMENT.md` ends its checklists with "Telephony, either
  target: blocked on route promotion ... expect `validate` and `compile` to
  reject the route today". Telephony routes validate and compile today, as
  section 1 shows. The site does not repeat the stale claim. Reported, not fixed
  here.

## 7. Per-page verification

Every page below was written from the sources named, with every claim checked
against code and every command run.

| Page | Anchor or source | How it was checked |
|---|---|---|
| index | scratch package | snippet validated for both targets |
| start/installation | `Makefile`, `go.mod`, `git remote` | build and version run; module path mismatch found (D3) |
| start/quickstart | a real `unmute init` run | full transcript: init, validate, dev, container healthy, page served |
| start/how-unmute-works | `docs/ARCHITECTURE.md` re-read against `internal/spec`, `internal/ir`, `internal/generate` | file lists taken from real compile output |
| build/one-agent | examples/simple-prompt | snippet validated; every field read from `internal/spec/package.go` |
| build/tools | examples/simple-prompt | snippet validated; tool fields and defaults from `internal/spec` and `internal/ir` |
| build/variables | examples/salon-support | both refusal messages reproduced by running the commands |
| build/two-agents | examples/subagents | snippet validated |
| build/tasks | examples/multi-task | snippet validated |
| build/task-groups | examples/task-groups | warning text taken from a real validate run; field values from `internal/ir/validate.go` |
| build/choosing-a-structure | all four structural examples | comparison checked against each package's YAML |
| dev/overview | `internal/cli/dev.go`, `dev_web.go`, captured help | every error message reproduced by running it |
| dev/console | `internal/cli/dev.go` | uv commands read from `consolePlan`; credential rule from `requireInferenceCreds` |
| telephony/overview | `docs/TELEPHONY.md` re-verified against `internal/cli/dev_telephony.go` | route matrix cross-read with the capability code |
| telephony/first-phone-call | examples/twilio-telephony-hello and its README | steps match the example page and the generated runbook |
| telephony/outbound-calls | examples/outbound-reminder and its README | `--to` refusals reproduced by running them |
| telephony/webhooks-and-tunnels | `internal/cli/dev_tunnel.go`, `dev_twilio.go` | messages quoted from the source; `--public-url` refusal reproduced |
| transfers/overview | `docs/TRANSFERS.md` re-verified against the examples | per-route table matches the capability rows |
| transfers/livekit | examples/livekit-human-transfer (LiveKit only) | compile report lines captured from a real run |
| transfers/pipecat-daily | examples/pipecat-human-transfer-daily (Pipecat only) | prerequisite block and dev refusal captured from real runs |
| transfers/pipecat-twilio | examples/pipecat-human-transfer-twilio (Pipecat only) | compile report lines captured from a real run |
| targets/overview | `internal/spec/package.go` Target struct | field list read from the struct |
| targets/pipecat | a real compile of examples/simple-prompt | file tree, pyproject, and runbook quoted from the build |
| targets/livekit | a real compile of examples/simple-prompt | same |
| deploy/going-live | `docs/DEPLOYMENT.md` re-verified against a real build | stale telephony claim found (D5) and not repeated |
| reference/cli/* | `specs/008-mintlify-user-docs/help.txt` | agreement test binds the pages to the binary |
| reference/agent-yaml | `internal/spec/package.go` plus `internal/ir/validate.go` | every enumerated value read from the validator |
| reference/targets-yaml | `internal/spec/package.go`, `regions.go` | same |
| reference/providers | `internal/target/catalog_*.go` | agreement test; refusal message reproduced by running it |
| reference/variables | `internal/ir/variables.go`, `internal/cli/dev_vars.go` | messages reproduced by running the commands |
| reference/secrets | `internal/ir/variables.go`, a real `.env.example` and `bot.py` | messages and generated code quoted from real output |

## 8. Unverified claims

- **A spoken turn was never completed.** `unmute dev` was run end to end on a
  scaffolded package: it compiled, built the image, started the container
  (healthy, Pipecat 1.5.0), served the dev page (HTTP 200) and answered
  `/api/session`. Talking to it needs a microphone and working model keys, which
  cannot be automated here. SC-002's "in under 15 minutes" is therefore
  unmeasured; the flow itself is verified up to that point.
- **SLNG model ids are verified against this repository, not against
  docs.slng.ai/models.** That page lists display names, not the id strings a
  package writes into `model:`. The two ids the site prints come from the
  shipped examples and from `internal/scaffold/scaffold.go`.
- **Telephony routes are documented as `provisional`, and no route was exercised
  with a live call during this work.** The dated live call evidence the site
  refers to is the repository's own record, not a run made here.
