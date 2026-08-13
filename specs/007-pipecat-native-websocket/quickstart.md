# Quickstart: validating Pipecat native WebSocket telephony

**Feature**: [spec.md](spec.md) | **Date**: 2026-08-13

Two halves, as in 006. The offline half needs no account and proves the
compiler's promises; it is what CI runs. The live half needs the accounts named
in the spec's Dependencies and proves the calls; its results are recorded,
dated, in `tasks.md`, and are what lifts the route's capabilities out of
provisional.

## Offline half (no credentials, no Python)

1. **The example compiles, and its shape is the route's whole point.**
   ```bash
   go run . validate examples/human-transfer-cloud-twilio
   go run . compile examples/human-transfer-cloud-twilio
   ```
   Expected: `✓ pipecat`, zero errors. The build directory contains **exactly
   the files a plain Pipecat Cloud build contains**: no helper, no compose
   telephony file, no new artifact of any kind. And that is asserted, not
   observed. `pcc-deploy.toml` carries `websocket_auth = "none"`. The README's
   telephony section contains the Bin markup, the org lookup command, and no
   occurrence of "tunnel", "ngrok", "cloudflared", or "helper".
2. **A pure-inbound package needs nothing.** A fixture with only
   `channels.phone.inbound` and no connection compiles; its compile report lists
   no carrier environment; its `.env.example` gains no carrier names.
3. **Untouched routes and examples stay untouched.**
   ```bash
   go run . compile examples/human-transfer-daily
   go run . compile examples/human-transfer
   ```
   Expected: byte-identical to their pre-feature builds. Deliberate example
   changes (spec FR-016): `examples/human-transfer-daily-twilio` is removed,
   replaced by the new example; `examples/telephony-hello`'s pipecat target now
   declares `cloud-websocket` and its build is read as a fresh golden, not
   compared to the old route's.
4. **The guards hold.**
   ```bash
   go test ./...
   ```
   Expected: zero failures, zero Python. The suite proves: the route row and
   emitter agree; the three markups render one service host and one wss URL;
   the regional URL follows the declared region; a SIP key on this route fails
   by name; outbound/transfer without a connection fails naming the three keys;
   the per-call env check and the compile report name the same set; the
   transfer TwiML dials an env name, never a literal, and carries the failure
   verbs after the `<Dial>`; the dev `--telephony` refusal names this route's
   nothing-local reality; and the runbook contract from
   [contracts/runbook.md](contracts/runbook.md) holds.
5. **Emitted Python stays lint-clean.**
   ```bash
   go test ./internal/generate -run TestPublicExamplesEmitLintCleanPython
   ```
6. **The platform-image gate still passes** (from 006, unchanged but
   load-bearing here): the emitted Dockerfile builds on the platform base image;
   optionally prove it end to end with Docker per 006's quickstart step 6.
7. **The dev flow's offline half.** `unmute dev --telephony` on this route is
   testable to the edge of the network without a phone: the command refuses
   cleanly when carrier credentials are absent (names, not values), and the
   number set/restore pair is covered by the existing dev machinery's tests.

## Local development half (one machine, one phone, no deploy)

Run `unmute dev --telephony` with the example's `.env` filled. Expected: one
command reports the local agent up, the cloudflared tunnel address, and the
number it borrowed. Call the number; the local agent answers. Stop the session
(try `ctrl-c` on purpose); read the number's voice configuration back and
confirm it is exactly what it was before. The production Bin, if one exists,
was never touched (spec US5).

## Live half (recorded, dated)

Prerequisites: Pipecat Cloud account, deployed agent (`status` reports
`ready`), a voice-capable Twilio number, two phones for the transfer drill.
Note what is absent from this list: any machine that stays running.

1. **Carrier setup per the runbook.** Fetch the organization name, create the
   Bin, point the number at it (off any trunk first; read the state with the
   dictated command rather than by calling).
2. **Inbound (spec US1, SC-001, SC-002).** Call the number. Expected: the
   spoken line within 2 seconds, the agent's greeting within 10 on a cold
   start. Record the delays. Confirm zero processes ran anywhere yours.
3. **Outbound (spec US2, SC-003).** Run the README's command with a reachable
   phone. Expected: it rings showing the operator's number; the agent greets on
   answer. Record the caller identity displayed.
4. **Cold transfer (spec US3, SC-004).** On an inbound call, ask for the
   person; expect the announcement, the destination ringing with ringback
   audible, the agent gone on answer. Then the drill that matters: destination
   declines; expect the spoken failure line and a fresh agent session, and
   confirm the caller was never in silence. Record attempts and outcomes,
   including the fresh-session limit behaving as documented.
5. **Failure mapping.** Break one thing at a time and confirm the
   troubleshooting map is honest: misspell the organization in the Bin (connect
   then drop → the map's row); stop short of `ready` and call (spoken line then
   drop); point the wss URL at the wrong region.
6. **Record.** Dates, delays, and counts go into `tasks.md`; capability rows
   lose `provisional` only for what was actually run.

## Rollback confidence

The feature is additive behind `transport: cloud-websocket`. The offline half's
step 3 proves every existing route's build is byte-identical, so shipping the
compiler change without lifting `provisional` is safe, exactly as it was for
006.
