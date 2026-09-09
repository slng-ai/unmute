# hotel-concierge

A concierge line for a fictional hotel in Madrid. A guest calls one number for
anything about the stay and the city. The agent answers as the concierge of the
hotel it is deployed for, knows the hotel's own facts from a hosted lookup,
recommends nearby places from Google Places through the organisation's hosted
request tool, reads the live web through the organisation's Firecrawl MCP
server when the question is about tonight or a menu, and ends the call when
the guest says goodbye.

It is the only example that targets `slng`, and the only one that produces no
runnable project. SLNG hosts the agent itself, so this package compiles to a
deployment body, not a project you start and keep running. It talks in a
browser web session and on an inbound phone call, both on the one `slng`
target.

One package serves any property. The hotel's name, neighbourhood, city and
website are template variables with defaults, so a phone call works with no
input and a web session can override them from the same deployment. The
hotel's identifier is pinned into the lookup, so the model never asks a guest
which hotel they are in and can never send the wrong one.

The agent also records one value the guest gives it. When a guest wants a text
with the summary of the call, the agent asks for the mobile number, reads it
back, and saves it only once the guest confirms. That number is a memory
variable: no session supplies it, the model fills it, and the call record
returns it. The text itself goes through SLNG's curated `send_sms`, with the
sender pinned by the package and the Twilio credentials read from the vault by
the platform.

Two facts about that text belong to the platform, not the package. SLNG sends
it to the number the call came from: the model writes the message and does not
choose the recipient, so the confirmed number is a record of the call, not the
address. And SLNG offers the tool on phone calls only: in a browser web session
the model has no text tool, and the prompt says so to the guest.

## What it shows

Everything the `slng` target accepts, in one package. Each row is one file or
one block to read.

| What it shows | Where | What to look for |
|---|---|---|
| A hosted code tool, referenced by name | `tools/hotel_info.yaml` | one `slng:` line; description and schema come from the platform |
| A hosted request tool with a package description | `tools/search_places_text.yaml` | `slng:` plus a `description:` that overrides the published one |
| A tool announcement | `tools/search_places_text.yaml` | `announce:`, spoken before the wait |
| An argument the model never sees | `tools/hotel_info.yaml` | `inject:` pins `hotel_id` to one fixed value |
| Named MCP tools | `tools/web_search.yaml` | `mcp.server` and two names under `mcp.tools` |
| A curated builtin | `tools/end_call.yaml` | `builtin: {id: end_call}` |
| A curated text message with a pinned sender | `tools/send_sms.yaml` | `builtin: {id: send_sms}` and `inject: from_number`; the model writes the message, SLNG sends it to the caller's number, on phone calls only |
| Template variables in the greeting and the prompt | `agent.yaml` `variables:`, `conversation.greeting.text`, `instructions.md` | four variables, each with a default |
| A value the model records during the call | `agent.yaml` `variables.caller_phone` | `source: conversation`, no default, a description the model reads; returned on the call record as a memory variable |
| A model fallback | `agent.yaml` `models.think` | `fallback:` naming `backup` on the primary |
| An inbound phone number | `channels.phone` and the deploy | trunk chosen at the terminal after the push |

What this target does not take, it refuses at validate by name: tasks, step
announcements, `prefetch:`, `confirm:` and declared shapes. Read the two salon
examples for those. Reading the caller's number, detecting voicemail and
telling the time are capabilities SLNG curates and attaches in the dashboard; a
package reaches only `end_call` and `send_sms` by name.

## Structure

- **Target.** `targets.yaml` names one target, `slng`, in `eu-central`. A
  push deploys an agent named `hotel-concierge-slng`: the package's name
  joined to the target's name.
- **Agent.** `agent.yaml` defines one agent, `concierge`, whose prompt is in
  `instructions.md`. It reasons with a Gemini model and falls back to a
  second Gemini model, and speaks and transcribes with the Deepgram models
  SLNG serves in Europe. The SLNG-hosted `slng/deepgram/...-en` copies run in
  other regions, and a push into `eu-central` refuses them by name.
- **Variables.** Four template variables, each with a default: `hotel_name`,
  `neighbourhood`, `city`, `hotel_website`. The greeting names
  `{{hotel_name}}` and the prompt names all four. A fifth, `caller_phone`, has
  `source: conversation` and no default: the model records it once the guest
  confirms the number, and it comes back on the call record.
- **Tools.** Every one is a reference: this target creates no tool, and no
  tool file carries a mirror.
  - `hotel_info` (`slng: hotel_info`): a Custom Code tool SLNG hosts, answering
    from a two-row table. Its one parameter is pinned to `lumbre-sol`; change
    that line to serve the other property.
  - `send_sms` (builtin): SLNG's curated text message. The package pins the
    sender with `inject: from_number`; the model writes the message, SLNG
    addresses it to the number the call came from and reads
    `TWILIO_ACCOUNT_SID` and `TWILIO_AUTH_TOKEN` from the vault itself. SLNG
    offers it on phone calls only.
  - `search_places_text` (`slng: search_places_text`): a request tool SLNG
    hosts, calling Google Places text search. The package's `description:`
    tells the model when to use it; the query is the model's.
  - `web_search` (MCP): reaches `firecrawl-mcp-2` for `firecrawl_search` and
    `firecrawl_scrape`, naming only the server and the two tools.
  - `end_call` (builtin): a capability SLNG curates, reached by name.
- **Session inputs.** None required. Every variable has a default, which is
  what lets an inbound phone call, which supplies no arguments, pass dispatch.
- **Secrets.** No `secrets:` block. The request tool's credential is SLNG's,
  discovered by `unmute deploy` from the published tool. The package reads no
  Vault entry by name: a `{{$NAME}}` Vault variable works in a prompt on this
  target, but Vault variables belong to a project, so every per-deployment
  value here is a template variable instead.
- **Compiled output.** `build/slng/`: `agent.json`, `README.md` and
  `compile-report.json`. That is all of it.

## Before you start

Run these commands from the repository root. To test the current branch, run
`make build` and use `./bin/unmute` in place of `unmute` below.

Install the push tool once, so it is on your PATH:

```bash
brew install slng-ai/tap/voiceai
```

Every `voiceai` command below runs against the same organisation `unmute`
deploys to:

```bash
export VOICEAI_API_KEY="$SLNG_API_KEY"
voiceai whoami
```

Check the organisation it names. A stored `voiceai login` profile can belong
to a different one, and that one will not have these resources.

This example references resources in the organisation it was written for.
Inspect yours with `unmute resources`, then make sure it holds each of these.
Unmute creates none of them from this package.

| Kind | Name | Created where |
|---|---|---|
| hosted request tool | `search_places_text` | SLNG dashboard, an API Request tool calling Google Places text search with one required `query` |
| hosted code tool | `hotel_info` | SLNG dashboard, a Custom Code tool holding the module below |
| curated tool | `end_call` | already on the platform |
| curated tool | `send_sms` | already on the platform, listed by `unmute resources` |
| Vault secrets | `TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN` | SLNG dashboard, Vault; SLNG's `send_sms` reads them by these names |
| MCP server | `firecrawl-mcp-2` | SLNG dashboard, the Firecrawl streamable HTTP server |
| inbound trunk | any free one | SLNG dashboard, Telephony; see [Receive phone calls](#receive-phone-calls) |

The `hotel_info` module, to paste into a new Custom Code tool named exactly
`hotel_info`. It has no dependencies and reaches no network, which is what a
Custom Code tool on SLNG is allowed to do:

```python
"""Facts about one hotel, by identifier.

Stands in for a property system. A Custom Code tool on SLNG has no network
access, so this answers from a table, which is what lets the example deploy
with nothing else provisioned. Times are written as the desk would say them
so the model can read them out unchanged.
"""

from pydantic import BaseModel, Field

HOTELS = {
    "lumbre-sol": {
        "name": "Hotel Lumbre",
        "address": "Calle del Arenal, 12, 28013 Madrid",
        "check_in": "from three in the afternoon",
        "check_out": "until noon",
        "breakfast": "in the courtyard restaurant, seven to half past ten, weekends until eleven",
        "front_desk": "staffed around the clock",
    },
    "lumbre-chamberi": {
        "name": "Hotel Lumbre Chamberí",
        "address": "Calle de Ponzano, 45, 28003 Madrid",
        "check_in": "from two in the afternoon",
        "check_out": "until eleven in the morning",
        "breakfast": "on the roof terrace, half past seven to ten",
        "front_desk": "seven in the morning to eleven at night",
    },
}


class Input(BaseModel):
    hotel_id: str = Field(..., description="Identifier of the guest's hotel")


class Output(BaseModel):
    found: bool = Field(..., description="False when the identifier is unknown")
    name: str = ""
    address: str = ""
    check_in: str = ""
    check_out: str = ""
    breakfast: str = ""
    front_desk: str = ""


def handler(input: Input) -> Output:
    row = HOTELS.get(input.hotel_id.strip().lower())
    if row is None:
        return Output(found=False)
    return Output(found=True, **row)
```

Publish it, then run it once on the platform to see it answer:

```bash
printf '{"hotel_id":"lumbre-sol"}\n' | voiceai tool run hotel_info --input - --confirm-side-effects
```

## How to run it

`validate` and `compile` work with no credential and no mirror to fetch first:

```bash
unmute validate examples/hotel-concierge
unmute compile examples/hotel-concierge --target slng
```

`unmute pull` does not apply: this package targets slng alone, and slng reads
no mirror.

Validate, compile, and push in one command. A real push needs a `voiceai`
release that supports a checked, resolved attachment; an older one is refused
with upgrade guidance before anything is written. This flow has been verified
with `voiceai 0.1.18`:

```bash
export SLNG_API_KEY=...
unmute deploy examples/hotel-concierge --target slng --dry-run
unmute deploy examples/hotel-concierge --target slng
```

`--dry-run` resolves every reference, checks the injected `hotel_id` against
the published `hotel_info` schema, and previews the attachment changes without
changing SLNG. Both commands write `build/slng/deploy-report.json`. Each real
deployment resolves the latest published tools again and attaches the versions
it checked. Rename the package's `name:` before deploying a separate test
agent; otherwise this command replaces `hotel-concierge-slng` if it already
exists.

### Talk to it in the browser

There is no `unmute dev` for this target. Open the deployed agent in the SLNG
dashboard, choose **Test**, then **Web session**, and allow microphone access.
The panel pre-fills every template variable with its default. See the
[test panel guide](https://docs.slng.ai/dashboard/agent-infra#test-your-agent).

To use your own LiveKit client instead, with the defaults:

```bash
cat > session.json <<'JSON'
{"arguments":{},"participant_name":"you"}
JSON
voiceai agents web-sessions create <agent_id> --file session.json
```

`unmute deploy` prints this command with the agent id already filled in. The
command returns `livekit_url` and `livekit_token`; it does not open a browser
or microphone. Connect a LiveKit client with those details to talk.

To hear the same deployment greet as a different hotel, override the
variables. The lookup stays pinned to one property in `tools/hotel_info.yaml`,
so the greeting and the neighbourhood change and the hotel's facts do not:

```bash
cat > session.json <<'JSON'
{"arguments":{"hotel_name":"Hotel Lumbre Chamberí","neighbourhood":"Chamberí, north of the centre","city":"Madrid"},"participant_name":"you"}
JSON
voiceai agents web-sessions create <agent_id> --file session.json
```

What to say, and what each exercise proves:

- "Where can I get tapas tonight?" The announcement, then one places call,
  then at most three names with a rating and a reason each, and no address.
- "What's the address of the second one?" One address, nothing else from the
  record.
- "When is check-out?" The lookup, with the pinned identifier, and no question
  about which hotel: "until noon".
- "Can you text me all that?" In a web session the agent has no text tool and
  says texting works from a mobile call. On a phone call it asks you to say
  the number you are calling from, reads it back in groups, and only after you
  say yes saves it and sends one text from the pinned sender to your number.
  The call record then shows `caller_phone` under `memory_variables`.
- "What's on at the Prado this week?" A spoken "let me check the web", a
  Firecrawl search, and a short answer naming the source.
- "What's the hotel's website?" The `hotel_website` default, spoken as words.
- "Goodbye." The call ends.

After the conversation, read the actual result rather than trusting what you
heard:

```bash
voiceai agents calls list <agent_id> --json
voiceai agents calls get <agent_id> <call_id> --json
```

The record shows each tool call. `hotel_info` received `hotel_id` from the
injection, and the model's own arguments carry none. After a text was sent,
`memory_variables` carries `caller_phone` with the number the guest confirmed,
so you can check what was recorded without reading the transcript.

### Receive phone calls

First follow [SLNG Telephony setup](https://docs.slng.ai/dashboard/telephony)
to route your carrier number to SLNG. `unmute deploy` offers a free inbound
trunk after a successful push; choose it, and call the number. A trunk another
agent holds is not offered: release it from that agent in the dashboard first,
or with one `voiceai agents update` PATCH setting `sip_inbound_trunk_id` to
null.

A call supplies no session arguments, so it gets every variable's default and
greets as the default hotel. That is why every variable here has one: a
required variable with no default would stop SLNG from attaching an inbound
trunk at all.

Attachment does not change carrier routing: a number still pointing at another
webhook will not reach SLNG. If no new call record appears, inspect the
carrier's call log and route.
