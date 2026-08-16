# Results: Upgrade Target Runtimes

**Feature**: 016-upgrade-target-runtimes | **Started**: 2026-08-16

## Human live-call verification (FR-012)

**This table is the release gate.** A release ships only when every row reads
pass. A failing row is a blocker, not a footnote.

Run each example, hold a real conversation, and exercise the behavior the
example exists to prove. Browser dev mode for most, telephony where the package
declares it:

```bash
bin/unmute dev examples/<name>
```

```bash
bin/unmute dev --telephony examples/<name>
```

On every run, check all five:

1. The agent greets and holds a back-and-forth conversation.
2. The distinguishing behavior fires (transfer, handoff, tool call, outbound
   call, inbound answer).
3. LiveKit only: the browser opens on its own, which proves the readiness
   marker still appears under the new run command, and container logs are
   readable colored output rather than JSON.
4. No deprecation warning appears in the container logs.
5. Ctrl-c tears the Compose project down without hanging.

| Example | Target(s) | Versions | Behavior to exercise | Result | Date | By |
|---|---|---|---|---|---|---|
| simple-prompt | livekit, pipecat | lk 1.6.10 / pc 1.7.0 | Conversation holds on both targets | | | |
| subagents | livekit, pipecat | lk 1.6.10 / pc 1.7.0 | Agent handoff fires | | | |
| salon-support | livekit, pipecat | lk 1.6.10 / pc 1.7.0 | Task flow completes | | | |
| multi-task | livekit, pipecat | lk 1.6.10 / pc 1.7.0 | Task switching works | | | |
| task-groups | livekit, pipecat | lk 1.6.10 / pc 1.7.0 | Grouped tasks run in order | | | |
| outbound-reminder | livekit, pipecat | lk 1.6.10 / pc 1.7.0 | Outbound call connects | | | |
| mcp-example | livekit, pipecat | lk 1.6.10 / pc 1.7.0 | MCP tool call returns | | | |
| livekit-human-transfer | livekit | lk 1.6.10 | Warm transfer completes with the briefing | | | |
| pipecat-human-transfer-daily | pipecat | pc 1.7.0 | Transfer completes | | | |
| pipecat-human-transfer-twilio | pipecat | pc 1.7.0 | Transfer completes over the carrier | | | |
| twilio-telephony-hello | livekit, pipecat | lk 1.6.10 / pc 1.7.0 | Inbound call answers | | | |

`livekit-human-transfer` is the most load-bearing row: warm transfer is the
feature whose floor moved from a silent constraint rewrite to a validation gate,
and its emitted code was previously verified only at 1.6.9.

## Watch items

From research R8, cheap to notice while already on a call. Neither is expected;
both have a named knob if they appear.

| Watch item | Symptom | Knob | Observed |
|---|---|---|---|
| Worker load threshold is 0.7 under the new run mode (was effectively unlimited) | Browser connects but no agent joins, on a CPU-starved container | `ServerOptions.load_threshold` | |
| Drain timeout on shutdown | Ctrl-c pauses during a live call | `ServerOptions.drain_timeout` | |
| Readiness marker under the new run command | Browser never opens by itself | the marker string in `internal/cli/dev_web.go` | |

## Automated verification (done)

Recorded here so the human run starts from a known state.

| Check | Result | Date |
|---|---|---|
| `make fmt`, `make lint`, `make build` | pass | 2026-08-16 |
| `make test` (L1-L3, zero Python, race) | pass | 2026-08-16 |
| Exact pin honored on both targets, verified on a real compile | pass | 2026-08-16 |
| Feature floor gates a below-floor version, verified on the real binary | pass | 2026-08-16 |
| Above-ceiling version names the fix | pass | 2026-08-16 |
| Emitted Python lint-clean across all examples (ruff) | pass | 2026-08-16 |
| No emitted file names a deprecated LiveKit run mode | pass | 2026-08-16 |
| `make smoke` (installs the ceilings, imports, instantiates) | | |
