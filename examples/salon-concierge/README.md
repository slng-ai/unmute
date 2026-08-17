# salon-concierge

The full Sage and Stone Salon project. Use this example before a release when
you want one package to exercise the main Unmute paths together:

- a verification entry agent and a typed customer task;
- a two-step booking task group for create, modify, and cancel;
- agent handoffs with shared customer context;
- a complaint agent with cold manager transfer;
- a chat agent whose only business tool is Firecrawl MCP;
- local Python tools backed by SQLite;
- browser audio and inbound phone calls on both code targets.

There is no outbound route. `channels.phone.outbound` is `false`.

## How the call moves

The concierge verifies the caller once, saves `customer_id` and `customer_name`,
then routes the full conversation silently. Every specialist can route directly
to any other specialist without announcing the internal handoff. The booking
preparation task can also leave immediately for a complaint or current-information
request without applying a booking change. Every route keeps the verified identity
and full history. No agent asks for the caller's name or phone again, or repeats
the full phone number, unless the caller says the identity is wrong.

The complaint specialist records the case with a local Python tool. It calls a
cold transfer when the caller asks for a manager or uses clearly and strongly
frustrated language. Frustration is a prompt decision, not a built-in sentiment
score. A cold transfer needs a live phone leg, so a browser caller hears a clear
fallback instead of a false success message.

Only `chat_with_me` lists `web_search`. The other agents and every task use local
Python tools or controls, so Firecrawl cannot be selected during verification,
booking, or complaint work.

## Local data

All seven local tool declarations point at [tools/salon.py](tools/salon.py). The
generated project copies that source for each tool, and every copy opens the
same owner-only `salon.db` in the runtime temporary directory. It stays outside
the generated project, so customer data cannot enter a later container build.
Python's `sqlite3` module is in the standard library, so there is no extra
package to install.

This is real local create, read, and update behavior, but it is demo storage.
The file survives separate conversations handled by the same worker and a
process restart may preserve it. It is container-local when deployed and can
differ across replicas. Replace it with a shared service before using this
package as a production booking system.

Run its fast behavior check directly:

```sh
python3 examples/salon-concierge/tools/salon.py
```

To reset the demo, stop the worker, delete
`/tmp/unmute-salon-concierge/salon.db` inside that worker, then start it again.

## What you need

Common values:

| Name | Purpose |
|---|---|
| `OPENAI_API_KEY` | reasoning model |
| `SLNG_API_KEY` | speech and transcription models |
| `FIRECRAWL_MCP_URL` | Firecrawl MCP address, normally `https://mcp.firecrawl.dev/v2/mcp` |
| `FIRECRAWL_API_KEY` | bearer token for Firecrawl |

`MANAGER_PHONE_NUMBER` is the cold-transfer destination in E.164 form. It is
needed only for inbound phone manager transfers, may stay unset for browser
sessions, and is checked before a phone caller hears the greeting.

The Pipecat target uses the `cloud-websocket` transport and also needs
`TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, and `TWILIO_PHONE_NUMBER` for a real
inbound call and transfer. `PIPECAT_CLOUD_ORGANIZATION` is supplied by the route
when deployed, not declared by the package. This route needs no `DAILY_API_KEY`.

The LiveKit target uses the `sip` transport and also needs
`SIP_TRUNK_HOSTNAME`, `SIP_AUTH_USERNAME`, `SIP_AUTH_PASSWORD`, and
`SIP_FROM_NUMBER`. Local development supplies `LIVEKIT_URL`, `LIVEKIT_API_KEY`,
`LIVEKIT_API_SECRET`, and `REDIS_URL`; LiveKit Cloud or your operator supplies
them after deployment.

Secrets stay in `.env`. No credential or phone number belongs in this package.
Keep `UNMUTE_LOG_LEVEL=INFO` for normal runs. Debug logs can include transcripts,
phone numbers, and tool inputs or results, so use fake customer data when you
need debug logging.

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
session does not read Twilio or SIP credentials.

## Take an inbound call

Pipecat receives Twilio Media Streams on its hosted `cloud-websocket` route:

```sh
bin/unmute dev --telephony examples/salon-concierge --target pipecat
```

LiveKit receives the same carrier through a Twilio Elastic SIP trunk:

```sh
bin/unmute dev --telephony examples/salon-concierge --target livekit
```

Follow each generated README's telephony setup before calling the number. The
phone route accepts inbound calls and can redirect the active caller to the
manager. It never starts an outbound conversation.

## Release conversation script

Use a future date when testing bookings. Start with a fresh demo database, then
run the first five rows in order in one conversation. Judge the called actions
and saved state, not exact wording.

| Check | What to say | Pass result |
|---|---|---|
| Unverified booking | “I want to book a haircut.” Do not give identity until asked. | Verification runs first. No booking action runs before it succeeds. |
| Verified booking | Give a new name and phone, pick an offered time, then explicitly confirm the full booking. | The customer is created once and exactly one active booking is saved with the offered slot. |
| Neutral complaint | “My last haircut was uneven, but I’d like the salon to fix it.” | One complaint is recorded for the same customer, the booking remains active, and no manager transfer starts. |
| Book then cancel | Ask to cancel the active booking and confirm. | The saved row is cancelled and no active booking remains. |
| Mid-booking complaint | Begin another booking, then before confirmation say, “Actually, I need to complain about my last visit.” | Booking stops without a write; customer care receives the same identity, history, and latest request without another verification question or internal handoff announcement. |
| Modify | Book another appointment, then ask to move it to another future date. | The same booking is updated atomically after confirmation. |
| Human transfer | On an inbound call say, “I want to speak to a manager.” | The active caller receives a cold transfer attempt. |
| Frustration | On an inbound call repeat that the issue is unresolved and refuse to continue with the agent. | The complaint specialist starts the same manager transfer. |
| MCP chat | Ask, “What is the latest official Python release?” | Chat searches through Firecrawl and answers from returned material. |
| Failure truth | Disable Firecrawl or leave the manager unavailable. | The active specialist states the limit and does not invent success. |

For longer real conversations, use the
[end-to-end harness](../../docs/HARNESS_TEST.md). Feature references:
[tasks](../../docs-site/build/orchestration/tasks.mdx),
[task groups](../../docs-site/build/orchestration/task-groups.mdx),
[handoffs](../../docs-site/build/orchestration/handoffs.mdx),
[MCP](../../docs-site/build/tools/mcp.mdx),
[transfers](../../docs-site/transfers/overview.mdx), and
[telephony](../../docs-site/telephony/overview.mdx).
