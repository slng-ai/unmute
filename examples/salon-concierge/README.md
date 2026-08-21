# salon-concierge

The full Sage and Stone Salon project. Use this example before a release when
you want one package to exercise the main Unmute paths together:

- a verification entry agent and a typed customer task;
- a three-step draft, confirmation, and apply group for create, modify, and cancel;
- agent handoffs with shared customer context;
- a complaint agent with cold manager transfer;
- a chat agent that answers open questions and hands off, with no tool of its own;
- local Python tools backed by SQLite;
- Coval tracing for release inspection;
- browser audio and inbound phone calls on three targets, covering both local
  telephony planes.

There is no outbound route. `channels.phone.outbound` is `false`.

## How the call moves

The concierge verifies the caller once, saves `customer_id` and `customer_name`,
then routes the full conversation silently. Every specialist can route directly
to any other specialist without announcing the internal handoff. The booking
preparation task can also leave immediately for a complaint or a general
request without applying a booking change. The apply step carries no handoff on
purpose, so a request raised there ends that step first, with whatever the
mutation actually returned, and names the request in `unserved_request`. The
booking specialist reads that off the returned result and routes the caller
without being asked twice. The apply step never refuses in place. Every route keeps the verified identity
and full history. During verification, the task spells the first name and surname,
reads every phone digit, and waits for a new clear yes before the customer action.
After verification, no specialist asks for or repeats those details unless the
caller says the saved identity is wrong. This readback checks what speech recognition
captured; it is not strong authentication such as an OTP.

On LiveKit, each shared booking result is labeled with its source task before
the next task starts, so the apply step can identify the confirmed draft.
That target overrides the shared reasoning profile with OpenAI's Responses API,
low reasoning, and HTTP transport. Pipecat keeps Chat Completions with reasoning
disabled. Both targets still use the same model ID.

The complaint specialist records the case with a local Python tool. It calls a
cold transfer when the caller asks for a manager or uses clearly and strongly
frustrated language. Frustration is a prompt decision, not a built-in sentiment
score. With no phone leg, a browser caller hears that direct transfer needs an
inbound call. On an established phone call, a carrier failure is different: the
explicit `hangup` policy ends the call because Pipecat `cloud-websocket` cannot
return after carrier takeover.

Every tool in the package is a local Python handler, so nothing has to be
reachable before the greeting. `chat_with_me` has no business tool at all: it
answers from what it knows, says plainly when it cannot check something, and
hands off for booking or complaint work. For a remote tool over MCP, see
[`examples/mcp-example`](../mcp-example/).

## Local data

All eight local tools share one process-specific SQLite database. The worker
deletes that database and its sidecars at startup, so every new worker starts
empty while verification, booking, and complaint tasks in that worker still
share data. Concurrent workers use separate files.

This is disposable test storage. Restart `unmute dev` for a clean run; do not
delete database files by hand. Use a shared database for a real deployment.

Run its fast behavior check directly:

```sh
python3 examples/salon-concierge/tools/salon.py
```

`make smoke` also proves that both generated targets start clean and share data
within one worker.

## What you need

Common values:

| Name | Purpose |
|---|---|
| `OPENAI_API_KEY` | reasoning model |
| `SLNG_API_KEY` | speech and transcription models |
| `COVAL_API_KEY` | Coval trace ingest |

`MANAGER_PHONE_NUMBER` is the cold-transfer destination in E.164 form. It is
needed only for inbound phone manager transfers, may stay unset for browser
sessions, and is checked before a phone caller hears the greeting.

The Pipecat target uses the `cloud-websocket` transport and also needs
`TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, and `TWILIO_PHONE_NUMBER` for a real
inbound call and transfer. `PIPECAT_CLOUD_ORGANIZATION` is supplied by the route
when deployed, not declared by the package. This route needs no `DAILY_API_KEY`.

The `livekit` and `pipecat_sip` targets both use the `sip` transport and need
`SIP_TRUNK_HOSTNAME`, `SIP_AUTH_USERNAME`, `SIP_AUTH_PASSWORD`, and
`SIP_FROM_NUMBER`. Local development supplies `LIVEKIT_URL`, `LIVEKIT_API_KEY`,
`LIVEKIT_API_SECRET`, and `REDIS_URL`; LiveKit Cloud or your operator supplies
them after deployment.

`pipecat_sip` is the same agent on that trunk with a Pipecat bot in the room
instead of a LiveKit worker. No managed platform sits behind it, so it emits no
deployment manifest: the containers a local run starts are the ones you run in
production. It receives calls only.

Secrets stay in `.env`. No credential or phone number belongs in this package.
Traces and debug logs can include caller speech, model input and output, phone
details, complaint text, and tool arguments or results. Use only fake identities,
fake phone numbers, and fake customer data for release tests, in a separate
Coval project. Do not send real customer data until its access and retention
rules are approved. Keep `UNMUTE_LOG_LEVEL=INFO` for normal runs.

## Booking confirmation boundary

Selection, confirmation, and mutation are separate tasks. Selecting a time only
finishes the draft step. The confirmation step then states the full proposal and
asks a new yes-or-no question; nothing said before that question counts. A clear
yes or matching “book it,” “move it,” or “cancel it” copies the exact draft into
the apply step. One unclear or interrupted answer gets one full restatement;
another unclear answer, an explicit no, or omitted confirmation yields zero
mutation. A topic change exits without applying the stale draft. This is a
model-and-workflow gate, not hard authorization. The local mutation functions do
not authenticate a caller or independently prove consent. A production booking
service must enforce authorization, consent, and idempotency at its own boundary.

## Empty LiveKit task responses

Generated LiveKit tasks retry up to twice when a full successful response has
neither non-whitespace text nor a tool call. Each retry starts immediately in
the same speech turn. It keeps the task state and model settings, and
adds a distinct temporary recovery instruction to a fresh copy of the
conversation context before each retry. After a task tool returns, recovery
keeps only `finish` instead of the full tool list. Non-whitespace text or an
allowed tool call stops recovery.
Errors and cancellations keep LiveKit's normal behavior.

If all three opening attempts are empty, the task speaks one fixed brief
failure, runs no action, and stays active for the caller's next turn. If an
empty reply follows a task tool, recovery can only call `finish`; it cannot run
another operation. Exhausting that recovery asks the caller to check the current
state before trying again. This applies only to generated LiveKit tasks. Normal
LiveKit agents and Pipecat output are unchanged.

## Validate and compile

```sh
make build
bin/unmute validate examples/salon-concierge
bin/unmute compile examples/salon-concierge
```

LiveKit warns that its task-group primitive is experimental. Both targets also
warn that inactivity and maximum-duration values still need driver-side range
checks. These are warnings, not silent downgrades.

The generated `build/<target>/README.md` is the deployment and carrier runbook.
Do not commit `build/`; it is disposable.

## Talk in the browser

Copy one generated environment template, fill the common values, and start a
target:

```sh
cp examples/salon-concierge/build/pipecat/.env.example examples/salon-concierge/.env
bin/unmute dev examples/salon-concierge --target pipecat
```

Use `--target livekit` to run the same browser journey on LiveKit. A browser
session does not read Twilio or SIP credentials. Pipecat browser development
uses `uv`; LiveKit uses Docker Compose.

## Test the local phone runtimes

Both targets run carrier-free. **No Twilio account, no number, no tunnel.**

Pipecat runs the generated phone-mode agent locally with `uv`, and the CLI is the
carrier: it places the call over loopback and connects your microphone to it.

```sh
bin/unmute dev --telephony examples/salon-concierge --target pipecat
```

Talk to it and ask for a manager. This package declares a **cold** transfer to
`manager_line`, so the handoff runs: the caller's leg leaves the agent, the
destination leg is recorded, and the run prints how far it got. A local run never
proves that a person answered, and it says so. Use headphones, or the agent hears
itself and interrupts itself. Talking needs `sox` (`brew install sox`); without it
the run plays a fixture and says which.

LiveKit starts Redis, LiveKit Server, LiveKit SIP, and the agent locally, and
prints an address and a per-run credential to dial from a softphone:

```sh
bin/unmute dev --telephony examples/salon-concierge --target livekit
```

The same trunk, with a Pipecat bot in the room instead of a LiveKit worker, is
dialled exactly the same way:

```sh
bin/unmute dev --telephony examples/salon-concierge --target pipecat_sip
```

The local check proves service health, trunk and dispatch setup, how the agent is
told about the call, and the conversation itself. It does not make the laptop reachable
from a carrier; a real call needs public SIP signaling and RTP, and an HTTPS
tunnel is not enough.

`--carrier` is the flag that reaches your own Twilio account: that adds the
tunnel and the temporary webhook change, and `--no-webhook` leaves the number
alone within it. [Calling your agent locally](https://docs.slng.ai/dev/local-telephony)
has the transfer support table and what each plane proves.

The package environment must contain `MANAGER_PHONE_NUMBER`, matching
`agent.yaml` and the generated `.env.example`. `SUPERVISOR_PHONE_NUMBER` is not
an alias.

## Release conversation script

Use a future date when testing bookings. Start a new worker, then run the first
five rows in order in one conversation. Judge the called actions and saved state,
not exact wording. Run the relative-date booking in the browser on both targets.
Repeat it on an inbound phone route only when that route is separately reachable.

| Check | What to say | Pass result |
|---|---|---|
| Unverified booking | “I want to book a haircut.” Do not give identity until asked. | Verification runs first. No booking action runs before it succeeds. |
| Relative-date booking | Give a fake first name, surname, and 10–15 digit phone number, confirm the complete identity readback, say “Book a haircut tomorrow afternoon,” pick an offered time, then explicitly confirm the full booking. | `find_or_create_customer` runs once after the identity yes and returns `created`. The trace then calls `get_current_date` before `check_availability`; the availability date is one day after the returned date, with no guessed-year invalid call. One `create_booking` returns `booked` for the offered slot. |
| No confirmation | Prepare a create, modify, or cancel request, then say no, stay silent, or change topic instead of confirming. | No booking mutation runs. An explicit no or a second unclear answer finishes unconfirmed; silence waits for inactivity handling, and a topic change hands off without completing the booking. |
| Neutral complaint | “My last haircut was uneven, but I’d like the salon to fix it.” | One `record_complaint` returns `recorded` for the same customer, no booking mutation runs, and no manager transfer starts. |
| Book then cancel | Ask to cancel the active booking and confirm. | One `cancel_booking` returns `cancelled`; a later `list_bookings` has no active booking. |
| Mid-booking complaint | Begin another booking, then before confirmation say, “Actually, I need to complain about my last visit.” | Booking stops without a write; customer care receives the same identity, history, and latest request without another verification question or internal handoff announcement. |
| Modify | Book another appointment, then ask to move it to another future date. | The same booking is updated atomically after confirmation. |
| Human transfer | On an inbound call say, “I want to speak to a manager.” | The active caller receives a cold transfer attempt. |
| Frustration | On an inbound call repeat that the issue is unresolved and refuse to continue with the agent. | The complaint specialist starts the same manager transfer. |
| No claimed lookup | Ask the chat specialist something it cannot know, such as another salon's prices today. | It says plainly that it cannot check, offers what it does know, and never claims to have searched. |
| Transfer failure truth | Try a manager transfer in a browser, then test an unavailable manager on a phone call. | Browser says an inbound phone leg is required. A carrier failure is not described as a browser limit, and the terminal policy may hang up. |

### Verification stress checks

Restart the worker before each case and run it in the browser on both targets.
Repeat it on a phone route only when doing a separate carrier test. Every
successful case must follow this order:

```text
verify_customer
complete first-name, surname, and phone readback
new clear caller confirmation
find_or_create_customer       exactly once, with the confirmed values
```

Test details given at once, a yes spoken before the readback, an interrupted
readback, a one-field correction, a fragmented phone number, an ambiguous
answer, and an explicit no. No customer action may run before the final yes;
the one later action must use the values that were read back. The Python
self-check covers name mismatch, and `make smoke` covers the clean restart.

### Exact answered and unavailable transfer calls

Start a new worker for every evidence run. Use this phone-only script once per
target with the manager answering and once per target with the manager declining
or not answering. Wait for each response.

1. “I need help with a complaint.”
2. “My name is Alex Test.”
3. “My phone number is plus one, five five five, zero one zero.” Pause, then
   say: “Eight eight four four.”
4. After the complete identity readback, say: “Yes, that is correct.”
5. “My haircut was uneven and I want to speak to a manager.”

An answered run needs observed two-way human audio. Carrier acceptance alone
does not prove that the manager answered. An unavailable run must end without a
new concierge greeting or a claim that the manager answered.

### Exact combined booking-to-manager call

Start a new worker first. Run this once in the browser and once by inbound phone
on each target. Wait for each response before speaking the next line.

1. “I want to book a haircut tomorrow at three. My name is Robin Taylor.”
2. “My number is five five five zero one zero.” Pause, then say: “Eight eight
   four four.”
3. After the complete identity readback, say: “Yes, that is correct.”
4. After booking preparation starts: “Actually, my last haircut was uneven. I’d
   like the salon to fix it.”
5. Only after customer care says the complaint was saved: “I want to speak to a
   manager.”

The trace must show this order:

```text
verify_customer
find_or_create_customer       exactly once
to_booking
manage_booking
get_current_date
check_availability
to_complaints                 from the active booking task
record_complaint              exactly once
to_manager                    exactly once
```

The final demo state is one customer, zero bookings, and one complaint. There
is no second verification, apply task, or booking mutation.

| Target and channel | Test | Pass result |
|---|---|---|
| Pipecat browser | Combined | The caller hears that an inbound phone call is required; no carrier action starts. |
| LiveKit browser | Combined | The caller hears that an inbound phone call is required; no carrier action starts. |
| Pipecat inbound | Answered | Phone B rings once, two-way human audio works, and the original agent stays ended. `transfer_started` proves carrier acceptance, not that a person answered. |
| Pipecat inbound | Unavailable | The call ends near the carrier timeout with no new greeting or claim that the manager answered. |
| Pipecat inbound | Combined | The exact action order and final state above pass before the terminal transfer. |
| LiveKit inbound | Answered | Phone B rings once, two-way human audio works, and the original agent stays ended. |
| LiveKit inbound | Unavailable | The call ends under the terminal policy with no new greeting or claim that the manager answered. |
| LiveKit inbound | Combined | The exact action order and final state above pass before the terminal transfer. |

### Release evidence

Record only sanitized IDs, counts, status, and carrier outcomes. Do not copy
names, phone numbers, transcripts, tool arguments, credentials, or raw traces.

| Date and revision | Target and case | Trace/session | Ordered evidence | Final state or carrier proof | Result |
|---|---|---|---|---|---|
| Pending | Pipecat browser combined | Pending | Pending | Pending | Pending |
| Pending | LiveKit browser combined | Pending | Pending | Pending | Pending |
| Pending | Pipecat inbound answered | Pending | Pending | Twilio child-leg status plus observed two-way audio | Pending |
| Pending | Pipecat inbound unavailable | Pending | Pending | Twilio child-leg final status plus observed terminal timeout | Pending |
| Pending | Pipecat inbound combined | Pending | Pending | SQLite counts/status plus Twilio child-leg status | Pending |
| Pending | LiveKit inbound answered | Pending | Pending | SIP/worker status plus observed two-way audio | Pending |
| Pending | LiveKit inbound unavailable | Pending | Pending | SIP/worker status plus observed terminal timeout | Pending |
| Pending | LiveKit inbound combined | Pending | Pending | SQLite counts/status plus SIP/worker status | Pending |

For longer real conversations, use the
[end-to-end harness](../../docs/HARNESS_TEST.md). Feature references:
[tasks](../../docs-site/build/orchestration/tasks.mdx),
[task groups](../../docs-site/build/orchestration/task-groups.mdx),
[handoffs](../../docs-site/build/orchestration/handoffs.mdx),
[transfers](../../docs-site/transfers/overview.mdx), and
[telephony](../../docs-site/telephony/overview.mdx).
