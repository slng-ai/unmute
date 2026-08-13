# Quickstart: validating the telephony setup feature

Prerequisites: Go 1.24, a clean `go test ./...` before starting. Runs 4 and 5 need real accounts: `lk` authenticated against the LiveKit project, the Twilio trunk from `examples/human-transfer/connections/twilio_sip.yaml`'s doc block, and the `.env` values (never committed, never pasted into any file or chat).

## Run 1: the retired name is gone (SC-002, FR-006)

```bash
go run . compile examples/human-transfer
grep -rn "LIVEKIT_SIP_INBOUND_TRUNK" examples/human-transfer/build/livekit/
```

Expected: exactly one match, the README retirement sentence. `.env.example`, `compile-report.json`, and both SIP JSON files have zero matches.

## Run 2: the artifacts hold their contracts (FR-004, FR-005)

```bash
ls examples/human-transfer/build/livekit/telephony-setup.sh
grep -n "trunk_ids" examples/human-transfer/build/livekit/sip-dispatch-rule.json
grep -n "UNMUTE_SIP_TRUNK_ID" examples/human-transfer/build/livekit/telephony-setup.sh
```

Expected: the script exists; `trunk_ids` carries `${UNMUTE_SIP_TRUNK_ID}` (present, non-empty); the script contains the empty-ID guard and no `source` of the env file. The README has the two-part runbook per [contracts/runbook.md](contracts/runbook.md), under the heading `## Telephony setup`, with the step count stated in its opening paragraph (SC-003).

Then the same check for the route that must not have it, using any package on the `livekit/connector/twilio` route: no `telephony-setup.sh`, no SIP JSON files, and no `## Telephony setup` section, because that route has no SIP trunk.

## Run 3: suite and goldens (SC-004)

```bash
make fmt && make lint && make test
```

Expected: green with zero Python. Golden diffs (livekit README, any fixture carrying telephony) were read line by line before the package's `-update-livekit` flag was accepted; a bare `-update` does not exist in `internal/generate`.

## Run 4: live provisioning (story 2, part of SC-001)

From `examples/human-transfer/build/livekit` with `.env` filled:

```bash
bash telephony-setup.sh
bash telephony-setup.sh   # second run proves idempotence: both records report "reused"
lk sip inbound list       # shows the trunk claiming the package's number
lk sip dispatch list      # shows the rule scoped to that trunk, agent name matching livekit.toml
```

Expected: first run creates trunk and rule; second run creates nothing. The dispatch rule's trunk list is never empty.

## Run 5: the four call flows (SC-001), after the Twilio part of the runbook

1. **Inbound**: call the package's number from a phone. The agent answers.
2. **Warm**: ask for a manager. The supervisor's phone rings, hears the briefing, says ready; caller and supervisor end up together (already proven 2026-08-12, run A1 of specs/003; re-run on an inbound call).
3. **Cold**: ask for billing. The caller is connected onward and the agent's session ends. Logs show the fired, referring, and completed lines from the specs/003 contract.
4. **Cold without toggles** (optional, destructive to the trunk config): disable Call Transfer on the Twilio trunk, repeat; expect the failed log line, and confirm the README maps it to the toggle.

Record dates and outcomes in tasks.md, as specs/003 did.

## Run 6: dev flow regression (SC-006, FR-009)

```bash
go run . dev examples/human-transfer --telephony
```

Expected: local trunk and dispatch records are created or reused exactly as before, with no mention of the retired variable anywhere in the output. Watch the ordering specifically: infrastructure services start, then the records are created, then the application. Losing that order means the startup gate was deleted rather than moved (T018).
