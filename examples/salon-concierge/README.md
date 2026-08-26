# salon-concierge

The full Sage and Stone Salon project. Use this example before a release when
you want one package to exercise the main Unmute paths together:

- a phone-only verification entry agent and a typed customer task;
- one booking task that handles create, modify, and cancel;
- agent handoffs with shared customer context;
- a complaint agent with cold manager transfer;
- a chat agent that answers open questions and hands off, with no tool of its own;
- local Python tools backed by one in-memory store;
- Coval tracing for release inspection;
- browser audio and inbound phone calls on three targets, covering both local
  telephony planes.

There is no outbound route. `channels.phone.outbound` is `false`.

## Tuned for turn latency

This package is tuned to cut the silence a caller hears between turns. Two
structural choices do that, and both work by cutting the number of LLM round
trips a turn needs, not by cutting what the agent can do:

- Verification asks for a phone number only, with no name to spell out loud.
  Spelling a name letter by letter over a transcriber is slow and error-prone,
  and the number alone finds the record.
- Booking is one task, not three. A separate draft, confirm, and apply step
  each cost their own round trip to finish, and the mutation tools already
  refuse a write that is not confirmed, so the split bought no extra safety.

### One router cache scope per prompt

Thinking goes through the SLNG Context Router, which can answer a repeated turn
from cache instead of asking the model. It judges which turns are worth treating
as repeatable, so a repeat served by the model is expected and not a fault.
`agent_id` is what keeps one project's learned answers apart from another's, and
this package writes one value: `optimized-salon-concierge-v3`.

The compiler adds the name of whoever is speaking, so this package sends six
scopes:

```
optimized-salon-concierge-v3:concierge
optimized-salon-concierge-v3:booking_specialist
optimized-salon-concierge-v3:complaint_specialist
optimized-salon-concierge-v3:chat_with_me
optimized-salon-concierge-v3:task.customer_verification
optimized-salon-concierge-v3:task.booking
```

Six and not one, because the cache key is the last exchange and does not include
the system prompt. Under one shared scope these agents were served each other's
lines: on 2026-08-21 the booking specialist's opening turn after a handoff came
back as the concierge's "what phone number should I use to look up your customer
profile?", from cache, in 1.27 ms, with no model call. The caller heard the agent
repeat itself.

This package used to carry `slng_pure_proxy: true` to stop that, which works by
turning cache serving off entirely and giving up the speed the router is here for.
It is gone. Cache serving is on, and nothing in the emitted output asks the router
to stop.

The values behind the prompt's placeholders are read again for every request, so a
value this call learns partway through is in the prompt from the next turn on
rather than being whatever it was when the call started. A name with no value yet
is still sent, as an empty string, because a placeholder the router was given no
value for is a 422 that would end the call.

### Why the phone number is a placeholder, and why its format matters

The booking specialist can tell the caller which number is on file, and that
number is `{{customer_phone}}` in its prompt rather than text this package renders
itself. The router refuses to store any answer containing a number, so an agent
that reads one back aloud normally pays the model for every one of those turns.
Through a placeholder it does not: the stored copy holds no number at all.

That only works while the router can find the value in the answer. It substitutes
back where the answer holds the value **character for character**, so the shape
the number arrives in decides whether anything caches at all. Measured 2026-08-24
against the live EU router, three reads per arm on fresh scopes:

| Value supplied | The model said it back | Served from cache |
|---|---|---|
| `555 070 1222` | unchanged | yes, 109ms on the third read |
| `+15550707444` | regrouped, so the value never appeared | never, 0 of 3 |

Nothing reports the failure. The caller hears a correct answer either way and the
hit rate is simply zero. That is why `customer_phone`'s description names the
format, and why `tasks/verify-customer.md` says what shape to return the number
in rather than leaving it to the model.

### One placeholder, in one prompt

`customer_id` stays out of every prompt: it is never spoken, so a placeholder
would buy no cache hit. The caller's name is not a variable at all, and neither
of those is only tidiness.

The compiler sends **one set of names per think profile**, the union of every
name any router-bound prompt on that profile references. All four agents and both
tasks share the `reasoning` profile here, so whatever this package declares and
references is sent on all six requests, not only on the prompt that says it.

That matters because of a rule in the router's own guide: its sharing scan refuses
any cached answer that still contains one of the values you sent for that call,
matching whole words and word beginnings. So every extra value narrows what can be
shared. A caller's first name is short enough to appear inside an unrelated word
in some answer somewhere; a phone number in this shape is not. One long, specific
value the agent actually says is the cheapest thing to send.

The counterpart holds too: the complaint and chat specialists never say a number,
so they carry no placeholder. That does not change what is sent, because the name
set is a union, but a prompt should not hold a value it has no use for.

Every think request also logs one line saying where its answer came from, so
whether any of this is working is a question you answer by reading the run:

```
slng router: scope=optimized-salon-concierge-v8:concierge source=cache layer=l2_exact request_id=req_...
```

Say the same thing twice in a row and watch `source=` change from `llm` to
`cache`. Expect `llm` on the first turn of every call, on every turn that calls a
tool, and on the turn after a tool result: none of those can be cached.

One rule to carry into your own package: a placeholder is for a value the agent
**says**. A value that changes **what the answer is**, the reply language above
all, belongs in the prompt text. Two callers who chose different languages would
otherwise share one cached answer and one of them would hear the wrong language.
Nothing warns you: the compiler cannot tell a spoken value from a steering one by
its name.

## How the call moves

The concierge verifies the caller once by phone number, saves `customer_id`,
then routes the full conversation silently. Every specialist can route directly
to any other specialist without announcing the internal handoff. The booking
task can also leave immediately for a complaint or open chat without applying a
booking change, since it holds those two handoffs itself. For any other request
it cannot serve, the task finishes first with whatever the last mutation
actually returned, and names the request in `unserved_request`. The booking
specialist reads that off the returned result and routes the caller without
being asked twice. Every route keeps the verified identity and full history.
During verification, the task reads every phone digit back once and waits for a
new clear yes before the customer lookup. After verification, no specialist
asks for or repeats the number unless the caller says the saved identity is
wrong. This readback checks what speech recognition captured; it is not strong
authentication such as an OTP.

On LiveKit, the target overrides the shared reasoning profile with OpenAI's
Responses API and a held WebSocket in place of a fresh handshake on every model
call, with reasoning still off. Pipecat keeps Chat Completions with reasoning
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
[MCP servers](../../docs-site/build/tools/mcp.mdx).

## Two knowledge bases

The salon has paperwork, and the agents quote it instead of guessing. Two folders
under `knowledge/`, each a separate in-memory index:

| Base | Folder | The tool | Which agent holds it |
|---|---|---|---|
| `refunds` | `knowledge/refunds/` | `look_up_refund_policy` | `complaint_specialist` |
| `services` | `knowledge/services/` | `look_up_salon_info` | `concierge` |

**An agent reaches a knowledge base by being given its tool, and that is the whole
access model.** The concierge can quote a price and cannot quote refund policy;
the complaint specialist is the other way round. Move a tool onto another agent
and that agent can quote that document, so tool attachment is the thing to get
right.

Both documents are fictional, and both PDFs are committed, so this works with no
setup beyond `OPENAI_API_KEY`.

Three things worth knowing before you change them:

- **Content is fixed until the next compile.** Each folder is read, split and
  embedded once when the agent starts, and held in memory. Editing a PDF changes
  nothing until you compile and deploy again.
- **The documents ride in the image.** `unmute compile` copies them into
  `build/<target>/knowledge/`, byte for byte, because a managed platform offers
  no mounted storage to read them from.
- **A scanned PDF fails at startup, not at compile.** Deciding whether a PDF
  yields text needs a parser the compiler does not have. A document with no text
  layer is named and skipped, and a base where nothing yields text stops the
  deployment. If a PDF is a photo of a page, run it through OCR first.

Ask the concierge a price and the complaint specialist a timescale. Then ask each
one the other's question: neither should answer from a document it was not given,
and neither should invent one.

## Local data

All eight local tools share one in-process store: dicts of customers,
bookings, and complaints guarded by one lock, held in a module that every copy
of `tools/salon.py` imports under the same name. Every new worker starts with
an empty store, and verification, booking, and complaint tasks in that worker
all see the same data. Concurrent workers do not share data.

This is disposable test storage with a hard ceiling: it lives in one process
and disappears when the worker restarts. Restart `unmute dev` for a clean run.
Put a real service behind these functions before a second replica exists.

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
| `OPENAI_API_KEY` | reasoning model, and the knowledge bases' embeddings at startup |
| `SLNG_API_KEY` | speech and transcription models |
| `COVAL_API_KEY` | Coval trace ingest |

`MANAGER_PHONE_NUMBER` is the cold-transfer destination in E.164 form. It is
needed only for inbound phone manager transfers, may stay unset for browser
sessions, and is checked before a phone caller hears the greeting.

The Pipecat target uses the `cloud-websocket` transport and also needs
`TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, and `TWILIO_PHONE_NUMBER` for a real
inbound call and transfer. `PIPECAT_CLOUD_ORGANIZATION` is supplied by the route
when deployed, not declared by the package. This route needs no `DAILY_API_KEY`.

The `livekit` target uses the `sip` transport and needs `SIP_TRUNK_HOSTNAME`,
`SIP_AUTH_USERNAME`, `SIP_AUTH_PASSWORD`, and `SIP_FROM_NUMBER`. LiveKit Cloud
supplies `LIVEKIT_URL`, `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET` and the Redis
behind its SIP service after you deploy; a self-hosted server needs them set.

Secrets stay in `.env`. No credential or phone number belongs in this package.
Traces and debug logs can include caller speech, model input and output, phone
details, complaint text, and tool arguments or results. Use only fake identities,
fake phone numbers, and fake customer data for release tests, in a separate
Coval project. Do not send real customer data until its access and retention
rules are approved. Keep `UNMUTE_LOG_LEVEL=INFO` for normal runs.

## Booking confirmation boundary

Selection and confirmation happen inside the one booking task. Picking a time
only finishes the selection step; it does not count as a yes. The task then
states the full proposal in one sentence and asks a new yes-or-no question.
A clear yes, or a matching “book it,” “move it,” or “cancel it,” saves the
exact proposal in the same turn with `confirmed` set to true. One unclear or
interrupted answer gets one full restatement; a second unclear answer, an
explicit no, or omitted confirmation saves nothing. A topic change exits
without saving the open proposal. This is a model-and-workflow gate, not hard
authorization. The local mutation functions do not authenticate a caller or
independently prove consent. A production booking service must enforce
authorization, consent, and idempotency at its own boundary.

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

Both targets warn that inactivity and maximum-duration values still need
driver-side range checks. These are warnings, not silent downgrades.

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

## Test the phone routes

You hear this agent on a phone once it is deployed. Both targets deploy to a
managed platform, and the emitted `build/<target>/README.md` has the carrier
steps for the route you chose:

```sh
bin/unmute compile examples/salon-concierge
```

Deploy the `pipecat` target to Pipecat Cloud and the `livekit` target to LiveKit
Cloud, do the carrier setup each runbook dictates, then call your number and ask
for a manager. This package declares a **cold** transfer to `manager_line`, so
the handoff runs: the caller's leg leaves the agent and goes to the destination.

Nothing local stands in for that. A carrier reaches an agent over publicly
routable SIP signalling and media ingress, which a laptop behind normal NAT does
not have. What you can do locally is the conversation itself:

```sh
bin/unmute dev examples/salon-concierge --target pipecat
```

That opens a browser session with the same prompt, tools and models, and no
phone involved. Use headphones, or the agent hears itself and interrupts itself.

The package environment must contain `MANAGER_PHONE_NUMBER`, matching
`agent.yaml` and the generated `.env.example`. `SUPERVISOR_PHONE_NUMBER` is not
an alias.

## Testing this on a real phone

Hold the handset to your ear. Do not use speakerphone, for the same reason the
browser loop asks for headphones: a phone leg has no echo cancellation, so an
open speaker sends the agent's own voice back into the microphone.

The Pipecat build protects the greeting for you, because that is where the echo
does the most damage. Everything after the opening line is still interruptible,
so on speakerphone the agent will still hear itself mid-call, be cut off, and
carry a garbled turn in its context for the rest of the conversation. A real
call on 2026-08-26 produced a first user turn reading `hi you've reached`, which
is the agent's own greeting, and every answer after that was built on it.

Deploy with `--min-agents 1`. This package carries two knowledge bases, and an
unbaked index is embedded at import before the server binds, which on a cold
start can push the container past the point where the session gives up. The
symptom is a session that never reaches the bot at all rather than a slow one.

## Release conversation script

Use a future date when testing bookings. Start a new worker, then run the first
five rows in order in one conversation. Judge the called actions and saved state,
not exact wording. Run the relative-date booking in the browser on both targets.
Repeat it on an inbound phone route only when that route is separately reachable.

| Check | What to say | Pass result |
|---|---|---|
| Unverified booking | “I want to book a haircut.” Do not give identity until asked. | Verification runs first. No booking action runs before it succeeds. |
| Relative-date booking | Give a fake 10–15 digit phone number, confirm the digit readback, say “Book a haircut tomorrow afternoon,” pick an offered time, then explicitly confirm the full booking. | `find_or_create_customer` runs once after the phone confirmation and returns `created`. The trace then calls `get_current_date` before `check_availability`; the availability date is one day after the returned date, with no guessed-year invalid call. One `create_booking` returns `booked` for the offered slot. |
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
complete phone-number readback
new clear caller confirmation
find_or_create_customer       exactly once, with the confirmed number
```

Test the number given at once, a yes spoken before the readback, an
interrupted readback, a digit correction, a fragmented phone number, an
ambiguous answer, and an explicit no. No customer action may run before the
final yes; the one later action must use the number that was read back. The
Python self-check covers invalid and malformed phone numbers, and `make smoke`
covers the clean restart.

### Exact answered and unavailable transfer calls

Start a new worker for every evidence run. Use this phone-only script once per
target with the manager answering and once per target with the manager declining
or not answering. Wait for each response.

1. “I need help with a complaint.”
2. “My phone number is plus one, five five five, zero one zero.” Pause, then
   say: “Eight eight four four.”
3. After the complete phone readback, say: “Yes, that is correct.”
4. “My haircut was uneven and I want to speak to a manager.”

An answered run needs observed two-way human audio. Carrier acceptance alone
does not prove that the manager answered. An unavailable run must end without a
new concierge greeting or a claim that the manager answered.

### Exact combined booking-to-manager call

Start a new worker first. Run this once in the browser and once by inbound phone
on each target. Wait for each response before speaking the next line.

1. “I want to book a haircut tomorrow at three.”
2. “My number is five five five zero one zero.” Pause, then say: “Eight eight
   four four.”
3. After the complete phone readback, say: “Yes, that is correct.”
4. After the booking task starts: “Actually, my last haircut was uneven. I’d
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
is no second verification or booking mutation.

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
| Pending | Pipecat inbound combined | Pending | Pending | Traced tool-result counts/status plus Twilio child-leg status | Pending |
| Pending | LiveKit inbound answered | Pending | Pending | SIP/worker status plus observed two-way audio | Pending |
| Pending | LiveKit inbound unavailable | Pending | Pending | SIP/worker status plus observed terminal timeout | Pending |
| Pending | LiveKit inbound combined | Pending | Pending | Traced tool-result counts/status plus SIP/worker status | Pending |

For longer real conversations, use the
[end-to-end harness](../../docs/HARNESS_TEST.md). Feature references:
[tasks](../../docs-site/build/orchestration/tasks.mdx),
[handoffs](../../docs-site/build/orchestration/handoffs.mdx),
[transfers](../../docs-site/transfers/overview.mdx), and
[telephony](../../docs-site/telephony/overview.mdx).
