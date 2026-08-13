# Quickstart: validating Pipecat carrier telephony

**Feature**: [spec.md](spec.md) | **Date**: 2026-08-12

Two halves. The offline half needs no account of any kind and proves the compiler's promises; it is what CI runs. The live half needs the accounts the spec's Dependencies name and proves the calls; its results are recorded, dated, in `tasks.md`, and they are what lifts the route's capabilities out of provisional (spec FR-020).

**Amendment, 2026-08-13.** This route no longer ships a public example. Feature
007 (`specs/007-pipecat-native-websocket`) added a route that does the same job
with nothing for the operator to host, and replaced this route's example with
`examples/human-transfer-cloud-twilio`. **Step 1's commands below no longer
resolve.** Everything they proved is proved instead against the fixture
`internal/testdata/daily_carrier`, which is this exact declaration shape, by
`go test ./internal/generate -run TestCarrier` and the tests in
`internal/generate/pipecat_carrier_telephony_test.go`. To compile this route by
hand, run `go run . compile internal/testdata/daily_carrier`. The route itself is
unchanged: same code, same rows, same guards.

## Offline half (no credentials, no Python)

1. **The carrier example compiles.**
   ```bash
   go run . validate examples/human-transfer-daily-twilio
   go run . compile examples/human-transfer-daily-twilio
   ```
   Expected: validate reports `✓ pipecat` with the `daily_dialout` prerequisite named (because the example declares outbound and a cold transfer) and zero errors. The build directory contains `telephony_helper.py`, `bot.py`, `pcc-deploy.toml`, and a README whose "Telephony setup" section states its counts up front.
2. **The no-carrier build is untouched.**
   ```bash
   go run . compile examples/human-transfer-daily
   ```
   Expected: byte-identical output to the pre-feature golden; no helper, no runbook section, no new env names.
3. **The guards hold.**
   ```bash
   go test ./...
   ```
   Expected: zero failures, zero Python. The suite now proves: the route row and emitter agree, granting nothing without an emitted path (agreement test extended); the helper is emitted exactly when a carrier is declared; the forward-once guard survives a double ready signal; the runbook contract (counts, carrier-free platform part, no transcribed IDs); the env split (no helper-only name in the secret set); `sip_username` on this route fails with the route named; a telephony call source on this route fails and the message names where that source does work; a warm transfer still fails and says which thing it means; the pairing errors on partial carrier declarations; and the dev refusal message for both Daily forms.
   The two authoring-surface checks the constitution mandates (the scaffold and the interactive console, spec FR-002a) are code inspections rather than new test signals: confirm each was examined and its finding recorded, in tasks T005a and T005b.
4. **Emitted Python is lint-clean.**
   ```bash
   go test ./internal/generate -run TestPublicExamplesEmitLintCleanPython
   ```
   Expected: pass (or clean skip where ruff is absent); the helper is inside the linted set.
5. **Goldens were read.** Every golden diff in the change is enumerated in the PR text; `-update-pipecat` was run once, deliberately, and the no-carrier goldens did not move.
6. **The image the platform starts.** Added 2026-08-13, because this is where the first live attempt actually failed and no test in the suite could have caught it from the text alone. Needs Docker, so it is not part of `go test ./...`:
   ```bash
   docker build --platform linux/arm64 -t unmute-pcc-check .
   docker run --rm -p 18080:8080 --env-file .env unmute-pcc-check
   curl -s -o /dev/null -w '%{http_code}\n' localhost:18080/readyz          # 200
   curl -s -o /dev/null -w '%{http_code}\n' -XPOST localhost:18080/bot -d '{}' -H 'Content-Type: application/json'   # 200, and the log shows bot() ran
   ```
   Expected: both 200. A 404 on either means the image is not built on the platform's base image, and no call will ever reach the agent.

## Live half (recorded, dated)

Prerequisites: a Pipecat Cloud account with the agent deployed and its secret set filled; a Daily domain with dial-out approved; a Twilio account with a voice-capable number; the runbook's carrier part completed; two reachable phones.

0. **The number points at the helper, not at a trunk.** Amended 2026-08-13, after a first attempt failed here: a number still attached to a SIP trunk ignores its webhook silently.
   ```bash
   twilio api core incoming-phone-numbers list --properties phoneNumber,trunkSid,voiceUrl
   ```
   Expected before calling: an empty `trunkSid` and a `voiceUrl` ending in `/call`.
1. **Helper up.**
   ```bash
   uv run telephony_helper.py
   ```
   Expected: refuses first with names when `.env` is incomplete (drill this once on purpose); starts clean when filled. Tunnel it and set the number's webhook per the runbook.
2. **Inbound (spec US1, SC-001, SC-004).** Call the number. Expected: hold audio within 2 seconds, agent answers and converses. Record the answer delay.
3. **Forward-once under the real signal.** Confirm from logs that one forward happened even if the ready event fired more than once.
4. **Outbound (spec US2).** Amended 2026-08-13: started against the platform, not the helper, because the helper has no call-placing endpoint any more (see [contracts/forwarding-helper.md](contracts/forwarding-helper.md)).
   ```bash
   curl -X POST https://api.pipecat.daily.co/v1/public/<agent>/start \
     -H "Authorization: Bearer $PIPECAT_CLOUD_API_KEY" -H 'Content-Type: application/json' \
     -d "{\"createDailyRoom\": true, \"dailyRoomProperties\": {\"enable_dialout\": true},
          \"body\": {\"direction\": \"outbound\", \"dialout\": {\"sip_uri\": \"sip:+15551230000@$SIP_TRUNK_HOSTNAME\"}}}"
   ```
   Expected: the target phone rings through the carrier trunk. Record what caller identity the recipient saw; this is the fact research F2 left provisional.
5. **Cold transfer (spec US3, SC-005).** On an inbound call, ask for the person; expect the spoken handoff, the destination ringing, the agent gone on answer. Then the failure drill: destination declines; expect the caller still connected and told. Repeat to the spec's counts across the session and record attempts.
6. **Failure mapping (runbook contract).** Break one thing at a time and confirm the troubleshooting map is honest: stop the helper (caller hears the carrier's failure, not eternal hold, on the next call), remove an allow-list entry (outbound fails, named in logs), remove a secret from the platform set (agent start fails by name).
7. **Record.** Dates, counts, and any step the run proved wrong go into `tasks.md`; the transfer reference's Status section is updated to match; capability rows lose `provisional` only for what was actually run.

## Rollback confidence

The feature is additive behind the carrier declaration. If the live half fails structurally, the no-carrier route and every other example are untouched (offline step 2), so shipping the compiler change without lifting `provisional` remains safe.
