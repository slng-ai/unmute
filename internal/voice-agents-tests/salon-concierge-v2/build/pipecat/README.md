# salon-concierge-v2

Generated Pipecat voice agent, compiled by `unmute` from a declarative spec. Do
not edit by hand — change the source spec and recompile.

## Quickstart

```sh
cp .env.example .env                      # then fill in your keys

uv run bot.py -t webrtc                   # web: open the URL it prints

```

Pipecat logs default to `INFO`. Set `UNMUTE_LOG_LEVEL=DEBUG` only in a
controlled environment because logs can contain caller speech, tool data, and
phone details.

## Latency measurements

`dev_metrics.py` prints one line per turn to stdout, so you can see where a slow
reply went. It prints nothing unless `UNMUTE_DEV_METRICS` is set, and `unmute
dev` sets it for you. Nothing is sent anywhere: the line goes to stdout and that
is all.

```text
UNMUTE_METRIC {"kind":"turn","seq":1,"e2e":1.057,"user_turn":0.412,"stages":[...],"tools":[...]}
```

Read them with `grep UNMUTE_METRIC` on the run's output. Times are seconds.
`e2e` is from the caller falling silent to the agent starting to speak, `stages`
carries the time to first byte for each service, and `tools` carries how long
each tool took, including tools reached through a remote tool source. Controls
are not listed there: a task or a transfer hands the conversation somewhere else
rather than doing a unit of work, and a task does not return until the
flow it started has finished, so its duration is that flow's, not a tool's.

A measurement Pipecat does not report is left out of the line rather than sent
as zero, so a field you do not see was not measured, not instant. Pipecat
reports no total streaming time per service here, only time to first byte.

## Agent handoffs

A handoff with `announce:` gives the exact sentence spoken once by
the active agent. Pipecat waits for that speech to stop before it activates the
receiving worker. The `requires:` guard runs first, so a refused handoff never
speaks the announcement. Without `announce`, the handoff stays silent.

The opening greeting belongs to call start only. A later handoff back to the
entry agent continues from the shared context and never repeats it.

## Guarded steps and handoffs

A control that declares `requires:` does not run until every named variable holds
a value. The refusal goes to the model, never to the caller: it names the unmet
variable and the control that supplies it, so the model fetches the value and
calls the control again in the same turn. The caller hears the next natural
question and never learns a guard fired.

Each guarded control also carries its requirement in its own description, so the
model usually collects the value during the earlier turns and the guard is never
reached at all.

**What you see in the log.** A refused control writes one line at normal level:

```text
prerequisite guard: step manage_booking refused, unmet customer_phone
```

and, on the attempt that reaches the retry limit:

```text
prerequisite guard: step manage_booking refused, unmet customer_phone, retry limit reached; asking the caller
```

Variable names and step names only. No caller speech and no variable value is
ever written, which matters when the identifier is something like a phone number.

After the limit the agent stops recovering quietly and asks the caller for the
missing value out loud, in its own words. It does not fall silent and it does not
end the call. That limit lives in this generated code, not in a prompt: a model
can be argued out of an instruction, and the guard exists for the case where it
already has been.

A repeated refusal with nothing in the log is not this guard. Look for a model
that is not calling the control at all.

## Facts resolved before the greeting

This agent resolves some values once per call, before it speaks, so the model
never spends a turn discovering them. Each entry below runs in the order
`agent.yaml` lists it.

| Entry | Reads | Lands in |
|---|---|---|
| `today` | the clock, in `Europe/Madrid` | `booking_date` |
| `caller` | the call's own `from_number` | `customer_phone` |
| `profile` | `look_up_customer`, a read-only lookup | `customer_name` |

**The budget.** The whole block has 2.0 seconds, which you will find in the
code as `_PREFETCH_BUDGET_S`. Past that it gives up, the
values keep their declared defaults, and the call greets on time. Nothing here can
fail a call: a lookup that times out or raises is logged and stepped over.

**An entry can do nothing, and that is normal.** An entry whose inputs are empty
is skipped. A call that carries no caller ID skips every entry that reads one,
and every entry downstream of those, so the agent behaves exactly as it would
have without this block at all. Compiling for a target whose route supplies no
call facts prints a warning naming each entry it will skip there.

**The clock is read once, at session start, in `Europe/Madrid`.** Not the
container's clock, which is UTC and would name the wrong day for anybody who is
not on it.

**A call that crosses midnight keeps the day it started on.** The value is read
once and never refreshed, so a call beginning at 23:55 still says that day at
00:10. This is deliberate: a value that changed underneath a conversation would
be worse, because the caller and the agent would stop agreeing about what
"tomorrow" means halfway through. If a call really can outlast midnight and the
date has to follow, read it in a tool instead of pre-fetching it.

**A pre-fetched value can need confirming.** `customer_name` (confirmed by `verify_customer`), `customer_phone` (confirmed by `verify_customer`) arrive
filled but not yet agreed to. Until the naming step has heard the caller agree,
such a value satisfies no `requires:` guard and appears in no prompt except that
step's own. So the agent reads the value back and asks for a yes rather than
acting on it, which is what stops somebody ringing from a friend's phone being
treated as the account holder.

**What you see in the log.** One line per entry, naming the entry and the values:

```text
prefetch today: resolved booking_date
prefetch profile: skipped, ...
```

A value is never written to the log, only its name, which matters when it is
something like a phone number.


## Tool-call failures

The generated direct-tool boundary keeps every call terminal. If the provider
sends an argument outside the declared input schema, the tool does not run and
the model receives one corrective result naming the allowed fields. If a
handler fails before returning, the full exception stays in the log and the
model receives one safe failure result. It must not claim success from either.

## What the caller cannot talk over

`bot.py` passes `user_mute_strategies=[MuteUntilFirstBotCompleteUserMuteStrategy()]` to the context aggregator.

The greeting is protected because this is a phone route, and a phone leg has no
echo cancellation. A caller on speakerphone sends your agent's own greeting back
through their microphone; the transcriber reports it as caller speech; the agent
interrupts itself about a second into the call. Worse than the truncated
greeting, that garbled turn stays in the model's context for the rest of the
conversation, so later answers are built on top of it. A real call before this
default existed opened with the model's first user turn reading
`hi you've reached`, which is the agent's own line.

Barge-in is still on for every turn after the opening line. To change that, set
`conversation.interruption.protect` in the package: name `tool_calls` as well to
keep a cough from abandoning a booking mid-write, or `[]` to ask for no
protection at all and accept the echo.


## Tools that speak first

A tool with `announce:` speaks that exact sentence as it starts, so the caller
is not waiting in silence while the call is in flight. The line is queued, not
awaited: the tool's own work starts straight away and the caller hears the
answer no later than they would without the line. The argument check runs
first, so a call the model got wrong stays silent. If the caller speaks over
the line, the tool's `interruption:` value decides what happens.

You will see the line in two shapes in `bot.py`, because a tool listed on an
agent and the same tool listed on a task are emitted differently. An agent tool
holds `FunctionCallParams` and pushes the frame through `params.llm`; a task tool
is a flows handler and queues it through `flow_manager.worker`. Same frame, same
contract, and nothing waits for playout in either.

## Turn taking

Two numbers decide how long a caller waits. **Both are spelled `stop_secs` in
`bot.py` and they are different fields**, so grep finds two and only this tells
you which is which.

**The floor is 0.2s**, in `VADParams(stop_secs=0.2)`. The VAD waits that
much silence before speech counts as stopped. It comes from the
package's `endpointing_delay`.

**The ceiling is 1.6s**, in `SmartTurnParams(stop_secs=1.6)` on the
end-of-turn analyzer. That is the longest the turn can run before closing
regardless, and it comes from `pace: balanced`.

The wait does not adapt to the caller on this target. Unlike the LiveKit driver,
which has a `dynamic` endpointing mode, this is the same fixed window on every
turn.

**Lowering the floor alone does not shorten a turn**, and on this target
widening it makes some turns *slower*. pipecat-slng asks the bridge to finalise
when the VAD reports the caller stopped; a final that has already arrived by then
finds no request outstanding, so the frame goes out unfinalized and Pipecat waits
out a flat 1.0s safety net instead. Measured over eight turns at 0.4s: every turn
whose transcript beat the window paid a flat 1.000s, and observed transcripts
arrive from 0.27s. That is why the floor here stays at 0.2s for every pace while
LiveKit's moves, and why the inconsistency is deliberate.

The end-of-turn analyzer (`LocalSmartTurnAnalyzerV3`) runs on every call. It is
constructed explicitly only so the ceiling is reachable; left implicit it sits at
its own 3s.


## Task returns

When a task returns, its internal turns stay out of the owner's conversation.
The completed task call stays in that conversation with the typed result,
so the same caller request does not start the task again.

A task may list a handoff in its own `handoffs:`. Calling it ends the active
task and any remaining task-group steps, then hands the caller directly to the
receiving agent. It does not return through the task owner. A task definition
has `tools:` and `handoffs:` and no other list, so a task or an escalation on a
task is unwritable rather than rejected: there is no key to put one in.

A task that lists no handoff still has a way out. Every node's
`role_message` here ends with one generated rule: when the caller asks for
something none of the step's tools can do, and no handoff on the step covers it,
call that node's `finish_<task>_<step>` function right away with the closest
result, naming that request in `unserved_request`. Every finish schema here
declares that field and never requires it, and a filled one rides the results
handback that tells the owning agent to take the request next. The rule does not
excuse skipping the step's own work, and a handoff the step declares wins over
it.

In a group with `context_scope: shared`, each exact typed result enters the
shared context before the next task starts. An isolated group carries no
results between steps. The final `merge: results` map is keyed by task name.

## Context a step or handoff starts with

A task or a handoff can set `context.history` to something other than
`full`. That changes what the step, or the agent taking the handoff, starts
with.

`history: full` needs no line here: one `LLMContext` is shared for the whole
call, so a step with nothing authored already has the running transcript,
tool records included.

`history: messages` keeps what the caller and the agent said out loud. Every
tool record is dropped, and a call leaves with its result: on this target the
call is a field on an assistant message and only the reply is its own entry, so
keeping one without the other is a request the provider rejects mid-call. A turn
that both spoke and called a tool keeps what it said.

`history: last_n` keeps the newest `max_messages` entries, tool records
kept. The cut lands at the front, so a leading tool result whose matching
call fell outside the window is dropped with it: a tool message with no
matching call is a request the provider rejects mid-call.

`history: reset` gives the step only its own instructions and its declared
variables. That is the floor: a reset step does not receive the call that
triggered it, the caller's own words included. Write a reset step to be
fully described by its declared values, like confirming a number or taking a
payment. A step that has to interpret what was just asked does not fit
`reset`, and there is no compiler error for getting this wrong.

`history: summary` is refused on this target. It exists only on LiveKit.

The ceiling is the same at every value: no `history:` setting is a privacy
control. Shortening what a step sees does not unsay what the caller already
said out loud.

`uv` installs the pinned dependencies on first run. The Pipecat dev runner
serves a WebRTC test client you can open in the browser.

That command is for talking to the agent right now, with no phone involved. For a
real phone call through your own number, see **Telephony setup** below: deploy,
then a few actions in your carrier's console, and nothing running on your side
at all.

While iterating on the source spec, `unmute dev <source-dir>` recompiles this
project and runs it for you behind a dev client.
## Knowledge bases

The agent answers from these documents rather than from what the model
remembers. Each folder was copied into this project by `unmute compile`.

- `refunds`, from `knowledge/refunds/`, embedded with `text-embedding-3-small` (needs `OPENAI_API_KEY`). `hybrid` search over passages of 90 tokens overlapping 20, returning 3 per lookup
- `services`, from `knowledge/services/`, embedded with `text-embedding-3-small` (needs `OPENAI_API_KEY`). `hybrid` search over passages of 220 tokens overlapping 40, returning 3 per lookup

Four things to expect at run time:

- **Content is fixed until the next compile.** The documents in `knowledge/` are
  what this project searches. Editing the originals in the source package changes
  nothing here until you compile and deploy again.
- **Indexing runs once per container, at import, not per session.** Pipecat Cloud
  imports `bot.py` and then calls `bot()` for each session, and it keeps a warm
  container for about five minutes after one ends, so the cost is amortised across
  every session that lands on it. A failure raises during import, so the container
  never reaches ready and the platform refuses the start. That is the intent: an
  agent that cannot read its documents should not take calls.
- **Bake the index into the image to make that import cheap.** Without it every
  cold container embeds the corpus before it can accept a session, and image
  warm-up is already the thing that decides how quickly a scale-up starts serving.
  Baked, that import is a disk read instead.

  ```sh
  docker build --build-arg KNOWLEDGE_BAKE=1 \
    --secret id=OPENAI_API_KEY,env=OPENAI_API_KEY .
  ```

  The credential arrives as a build secret, so it is never written into a layer.
  Leave the flags off and the image still works, and the log says at startup that
  it is embedding instead. A lookup still embeds the caller's question, so the run
  time needs the same credential either way: baking removes the corpus cost, not
  the query cost.
- **A document with no text layer is named and skipped at startup.** A scanned PDF
  is an image of a page, and nothing can extract text from it without OCR. If some
  documents are readable the agent still starts and the log says how many of how
  many were indexed. If none are, the import fails.
- **A lookup that fails does not end the call.** The agent is told the lookup is
  unavailable and says so. The reason goes to this container's log and never into
  the model's context, because a model handed a provider error can read it out
  loud.

**Importing `bot.py` builds the indexes, so a plain import needs a working
embedding credential.** That is deliberate: the platform imports this module and
its readiness probe does not pass until the import finishes, which is what keeps
a caller from waiting for indexing. It does mean a tool that imports the module
just to inspect it — a type checker, an editor, a script — reaches the embedding
service too. Give it a valid key, or stub `knowledge._embed_<base>` before the
import.

Both `knowledge.py` and `knowledge/` have explicit `COPY` lines in the Dockerfile.
That is not decoration: this image copies named files only, because `/app` belongs
to the base image, and leaving either one out gives a container that starts,
raises, and never reaches ready while its own log looks healthy.

Nothing here writes outside the container. The index is in memory for the life of
the process and is not persisted anywhere.

## Input variables

This agent expects values supplied when the call starts:

- `appointments` (string)
- `booking_date` (string)
- `caller_reason` (string)
- `complaints` (string)
- `customer` (string)
- `customer_name` (string)
- `customer_phone` (string)

Locally, pass them with `unmute dev` on the source package:

```sh
unmute dev <source-dir> --var appointments=... --var booking_date=... --var caller_reason=... --var complaints=... --var customer=... --var customer_name=... --var customer_phone=...
```

In production the same values ride the runner's call-start payload as one flat
JSON object, and the bot also reads them from the `UNMUTE_CALL_START`
environment variable when the payload carries none.

## Declared state

Every agent prompt and every task prompt in this project ends with a block the
compiler wrote:

```text
Conversation info:
What this call has established so far. Read it rather than re-reading the conversation, and never ask for something already recorded here.
1. Booking date: {{booking_date}}
2. Caller reason: {{caller_reason}}
3. Customer: {{customer}}
4. Appointments: {{appointments}}
5. Complaints: {{complaints}}
```

No prompt file contains it. Adding a field to a shape in the source package
changes every prompt here with no prompt file edited, so the model is told what
the call has established rather than being left to re-read the conversation.

The values it names:

- `booking_date` (`Date`)
- `caller_reason` (`list[Literal["create_booking", "modify_booking", "cancel_booking", "request_informations", "complain"]]`)
- `customer` (`Customer | None`)
- `appointments` (`list[Appointment]`)
- `complaints` (`list[Complaint]`)
- `customer_phone` (`Phone`), which the caller has to agree to in the `verify_customer` step

What to expect when you read a prompt or a trace:

- A value with no contents renders as the words `none recorded yet.`, never as
  an empty list or a null. A step reading an empty structure cannot tell "not
  yet known" from "known to be nothing".
- A value with contents renders as compact JSON, never as a Python repr.
- A value the caller has not agreed to renders in its confirming step's prompt
  and nowhere else on the call.
- No declared secret renders in the block at all.
- One value is bounded at `_STATE_VALUE_MAX` characters. A longer one is
  shortened and logged, never dropped and never allowed to end the call.

Each declared shape is a generated Pydantic class in this module. A step's
result is validated against it before anything is recorded, so a value outside a
declared set never enters the state: the previous contents stay as they were and
the model is told which field and what was allowed, which is what lets it
correct itself on the next turn.

A text type with a validated shape (`Phone`, `Date`, `Time`, `Id`) is a `str` in
the schema the model is sent, with its shape checked by a validator. That is
deliberate: a `pattern` in the schema survives the strict tool-schema converter
and the provider rejects it, so the shape is checked here instead.

## Required environment

Set these before running (values are never written into the project):

- `LANGFUSE_BASE_URL`
- `LANGFUSE_PUBLIC_KEY`
- `LANGFUSE_SECRET_KEY`
- `MANAGER_PHONE_NUMBER`
- `OPENAI_API_KEY`
- `SLNG_API_KEY`
- `TWILIO_ACCOUNT_SID`
- `TWILIO_AUTH_TOKEN`
- `TWILIO_PHONE_NUMBER`

One group, because one thing reads them: the deployed agent. There is no second
side on this route, so every name above belongs in the platform secret set.

## Deploy to Pipecat Cloud

Authenticate once first with `pipecat cloud auth login`. After that, one command
does the first deploy and every later one: `pipecat cloud deploy` builds the image
in the cloud from this project's `Dockerfile` and deploys it. That is why the
manifest names no image; naming one switches cloud builds off.

Create the secret set first, because the manifest already references it and a
deploy cannot start without it:

```sh
cp .env.example .env            # then fill in the values
pipecat cloud secrets set salon-concierge-v2-pipecat-secrets --file .env --region eu-central
pipecat cloud deploy
pipecat cloud agent status salon-concierge-v2-pipecat
```

Run the same `secrets set` command again to change a value. If that set name is
already taken (secret-set names are globally unique across the platform), create
yours under another name and pass `--secrets <other-name>` on the deploy command,
which overrides the manifest.

Deploying the same agent name again updates the existing agent rather than making
a second one. Sessions already running finish on the old image, and a deploy that
fails leaves the previous ready version serving traffic. `status` reporting
`ready` is the deploy actually being usable.

### This agent's name

**salon-concierge-v2-pipecat** is `name:` in your `agent.yaml` joined to the `pipecat`
target it was compiled for, and it is `agent_name` in `pcc-deploy.toml`. Agent
names are unique across your organisation and the deploy above updates whichever
agent matches, so this name says which package this is rather than which
platform it runs on. The secret set follows the same name,
because a secret set is per-organisation in the same way.

Renaming it in the source package does not move this deployment. The next deploy
creates a second agent and the old one keeps running and keeps billing, so
delete it with `pipecat cloud agent delete salon-concierge-v2-pipecat` and
create the secret set again under the new name.

The manifest holds **1** instance ready, from `warm_instances` on
this target in `targets.yaml`. That is a standing cost and it is what removes the
cold start. Change the number in the source package and recompile: this file is
rewritten on every compile, so the declaration belongs there and reaches every
later deploy on its own.

### Reading this agent's log

```sh
pipecat cloud agent logs salon-concierge-v2-pipecat -n 250
pipecat cloud agent logs salon-concierge-v2-pipecat -s <session-id>
```

**Do not pass `-l DEBUG`.** It filters to *only* that level rather than setting a
minimum, so it hides the `ERROR` line that says why a session died. Run with no
`-l` at all.

The CLI wraps output at 80 columns, which corrupts its JSON output. For machine
reading, widen it first:

```sh
COLUMNS=100000 pipecat cloud --output json agent logs salon-concierge-v2-pipecat -n 2000 -s <session-id>
```

`pipecat cloud agent sessions salon-concierge-v2-pipecat` lists the sessions themselves, which is
how you tell a session that never started from one that failed inside. A session
marked `Complete` with a blank `Bot Start Seconds` never reached `bot()` at all.

### Region

This agent deploys to `eu-central`, from the manifest. Its secret set
carries the same region, because a set is region-scoped and an agent can only
read one in its own region.

A second region is a second agent, because agent names are globally unique
across regions:

```sh
pipecat cloud secrets set salon-concierge-v2-pipecat-<region>-secrets --file .env --region <region>
pipecat cloud deploy salon-concierge-v2-pipecat-<region> --region <region> --secrets salon-concierge-v2-pipecat-<region>-secrets
```

`pipecat cloud regions list` prints the regions available to you.

## Telephony setup

**Nothing runs on your side and nothing is hosted by you.** Your carrier streams
the call's audio straight to Pipecat Cloud, which starts this agent.

The whole setup is **four steps**: three in your carrier's console, plus one
lookup command you can run anywhere. In production there is nothing of yours to
run, host, or keep awake.

### Who does what

Two parties, and neither of them is you.

- **Twilio** owns the phone number and holds the small piece of call markup that
  sends a call's audio to the platform.
- **Pipecat Cloud** runs this agent and receives that audio directly.

The carrier part is yours to do, because nobody but you can sign in to your
carrier account, so every step is written out.

### At your carrier (Twilio)

Do these in the [Twilio Console](https://console.twilio.com), in this order.

1. **A voice-capable number you own in this Twilio account.** Phone Numbers,
   Manage, Buy a number, with the Voice capability. Skip this if you already own
   the number you want. If another target in this repository already uses a
   Twilio number, the same account and the same number serve this one.

   This same number is also what the person you call sees, and there is nothing
   else to enable on it: voice capability is the whole requirement. See
   **What the recipient sees** below for the two account-level things that can
   still refuse a call.

   Take the number off any SIP trunk first. A number attached to a trunk ignores
   its voice configuration completely, and it does so silently, so the markup you
   create in step 3 would never be consulted. See **One number serves one target
   at a time** below.

2. **Your organization name**, the one value the compiler cannot know:

   ```sh
   pipecat cloud organizations list
   ```

   **You want the machine slug, not your display name.** The output has more than
   one column and their headings differ between CLI versions, so go by the shape
   of the value rather than by the heading:

   ```text
   Organization Name   Organization ID           Active
   ─────────────────────────────────────────────────────
   your-display-name   three-random-words-12345  active
                       ^^^^^^^^^^^^^^^^^^^^^^^^ this one
   ```

   The value you want looks like `three-random-words-12345`: lowercase words
   joined by hyphens, ending in a number, never a space in it. Depending on your
   CLI version that column is headed **Organization ID** or just **Name**. It is
   never the human-readable name you chose, and never carries the `(active)`
   marker.

   Getting this wrong is the most common way this route fails, and it fails in the
   least helpful way: the caller hears the spoken line, then silence, and **your
   agent's log stays completely empty**, because a connection the platform refuses
   never reaches your agent at all.

3. **Create a TwiML Bin** holding exactly this, with your organization name in
   place of `YOUR_ORGANIZATION`. Console path: TwiML, TwiML Bins, then the plus
   button. Name it anything; the content is what matters.

   ```xml
   <?xml version="1.0" encoding="UTF-8"?>
   <Response>
     <Say>Connecting you now.</Say>
     <Connect>
       <Stream url="wss://eu-central.api.pipecat.daily.co/ws/twilio">
         <Parameter name="_pipecatCloudServiceHost" value="salon-concierge-v2-pipecat.YOUR_ORGANIZATION"/>
       </Stream>
     </Connect>
   </Response>
   ```

   Four things in there are deliberate:

   - **The spoken line comes first.** Starting this agent takes a few seconds
     from cold, and a caller who hears nothing hangs up. Delete the `<Say>` line
     once you keep a warm instance (this deployment holds 1 ready).
   - **The address is already the right one**, including the region: this file
     was generated for this deployment, so paste it as it is rather than copying
     one from a guide.
   - **The agent name is already filled in.** `YOUR_ORGANIZATION` is the only
     substitution you make.
   - **There is nothing else in it.** A `<Parameter>` reaches this agent only if
     the emitted code reads one by that name, and on an inbound call that is the
     service host alone. Adding the caller's number here is a common suggestion
     and it does nothing: no module in this package reads it.

4. **Point the number at that Bin.** Phone Numbers, Manage, Active Numbers,
   your number, Voice Configuration, "A call comes in", choose TwiML Bin, pick
   the one you just made, Save.

   Read the state back rather than testing by ear:

   ```sh
   twilio api core incoming-phone-numbers list \
     --properties phoneNumber,trunkSid,voiceUrl
   ```

   An empty `trunkSid` is what you want. A `trunkSid` with a value means the
   trunk still owns the call and your Bin is never reached.

### One region, three places

This agent's region is `eu-central`, declared once in the source
package's `targets.yaml` as `deployment_region`. Three things are derived from that
one declaration, and the platform requires them to agree:

| What | Where it lands | Why it has to match |
|---|---|---|
| where this agent runs | `region` in `pcc-deploy.toml` | it is the deployment |
| where its secrets live | the `--region` on the `secrets set` command | an agent can only read a secret set from its own region |
| where your carrier streams to | the `wss://` address in the markup above | a regional stream endpoint routes **only** to agents deployed in that region |

Following this file keeps all three in step, because all three are rendered from
the same declaration. To move region, change that one line in `targets.yaml`,
recompile, and re-paste the address into your Bin. `pipecat cloud regions list`
prints what is available to you.

Two things bite when you move an agent that already exists:

- **Agent names are globally unique across regions**, so `salon-concierge-v2-pipecat` cannot exist
  in two regions at once. Either delete the old deployment first, or deploy the new
  region under another name and change the agent half of the service host in your
  Bin to match it.
- **A secret set is region-scoped and its name is global.** Create one for the new
  region under a new name and pass `--secrets <that name>` on the deploy, or delete
  the old set first.

Where these steps come from, if you want the upstream reading:

- The working example this route is modelled on:
  <https://github.com/pipecat-ai/pipecat-examples/tree/main/twilio-chatbot>
- The wire protocol between your carrier and the platform:
  <https://www.twilio.com/docs/voice/media-streams/websocket-messages>
- What a TwiML Bin is and how to create one:
  <https://help.twilio.com/articles/360043489573-Getting-started-with-TwiML-Bins>
- Regions, and the rule that the stream endpoint must match the deployment:
  <https://docs.pipecat.ai/pipecat-cloud/guides/regions>

### On this side

**Nothing, in production.** That is the whole section, and the absence is the
feature. Deploy per **Deploy to Pipecat Cloud** above, wait for `ready`, and the
number works. (This package places or redirects calls, so fill the
names under **Required environment** and push the secret set before deploying.)

### Hear it before the phone

`unmute dev <source-dir>` opens a browser session with this same agent: the same
prompt, the same tools, the same models, and no phone involved. Get the
conversation right there.

The phone leg itself starts where that stops, and it starts at a deployed agent.
Deploy, do the **Wire up Twilio** steps above, then call your number. A carrier
reaches you over the public internet, so nothing running on your laptop can
stand in for that.


### What a transfer does, and what it cannot do

When the agent hands the caller to a person, one request replaces the live call's
markup at your carrier: a spoken line, then the destination dialled with ringback
audible, then a final hangup that ends the original call after that destination
leg finishes. The
destination is read from the environment at call time, so no phone number is
written into this project.

This is deliberately a cold transfer. If the person hangs up first, the original
call ends. If the destination declines or never answers, the original call also
ends after the dial timeout. No fresh agent starts with an empty conversation.

Pipecat `cloud-websocket` requires explicit `on_unavailable: hangup`; it cannot
reconnect the original media stream. Omitting the field resolves to
`return_to_caller`, so validation refuses it on this route. A successful carrier
REST update means the transfer has started, not that the destination answered;
the tool result is `transfer_started`.

**Warm transfer is not supported on any Pipecat target in Unmute.** Use the
LiveKit SIP route when the agent must brief the person before connecting the
caller. See the telephony and transfer overviews in the Unmute documentation
site.

### Security, in one paragraph

This route's deploy manifest sets `websocket_auth = "none"`, because a static
piece of markup in a carrier console cannot fetch a token first. What limits who
can start a session, then, is knowing the `salon-concierge-v2-pipecat.<your organization>`
string your markup carries. Treat that string like a capability: it is not a
secret in the cryptographic sense, but anyone holding it can open sessions you
pay for. This is the platform's documented model for carriers that connect
directly.

### Every step, in order, from a source change to a phone call

Run it from the **source package**, not from this directory: this directory is
generated output and recompiling overwrites it.

**1. Recompile after any source change.**

```sh
unmute validate <source-dir>
unmute compile <source-dir>
```

**2. Move into the build directory.**

```sh
cd <source-dir>/build/pipecat
```

**3. Fill the environment and push it.** `.env` is not regenerated over, so this
survives recompiles:

```sh
cp .env.example .env      # then fill in the values
pipecat cloud secrets set salon-concierge-v2-pipecat-secrets --file .env --region eu-central
```

**4. Deploy, and wait for ready.**

```sh
pipecat cloud deploy
pipecat cloud agent status salon-concierge-v2-pipecat
```

Do not place or expect a call until `status` reports `ready`.

**5. Do the carrier part once**, per **At your carrier** above. It only changes
again if you change region or agent name.

**6. Test an incoming call.** Ring your own number. Expected: the spoken line
within a couple of seconds, then this agent.

**7. Test a transfer, both ways.** On a call, ask for the person this agent can
hand you to. Expected: the accepted update ends the agent's stream, then the
carrier speaks the line and rings the destination. Let the person **hang up
first**: expected is the original call ending, with no new agent greeting. Then
run it a third time with
the destination **declining or not answering**: expected is the original call
ending after the dial finishes. Those last two runs cover the ordinary and
failure paths.

One log, and only one: `pipecat cloud agent logs salon-concierge-v2-pipecat`. There is no
second place to look on this route, because there is no second thing running.

### If the call feels slow

The platform writes the numbers into that log, so read them before changing
anything. Two lines answer almost every latency question:

```text
[pcc-observability] First bot speech | latency=2.297s
[pcc-observability] Latency breakdown | events: ["...STT: TTFB 17.651s", "...LLM: TTFB 2.215s", ...]
```

- **First bot speech** counts from inside the process only. If it reads about two
  seconds and your caller waited much longer, the difference was the container
  starting: see the cold-start row above.
- **The breakdown** attributes each turn to a service by name. One service far
  above the others is that provider, not this project and not the platform: take
  it up with them, or bind a different provider for that role in the source
  package and recompile.
- **End-of-turn detection sits between the two.** The pipeline waits to be sure
  the caller has finished before it answers, which costs a second or two per turn
  on purpose. It is the difference between an agent that interrupts and one that
  feels polite.

### One number serves one target at a time

A number's Voice configuration is **either** a webhook or a TwiML Bin or a SIP
trunk, never two at once, so there is never a question of which answers:
whichever the number points at is the one that gets the call. Both of your agents
can stay deployed the whole time, and taking one down is not how you switch
between them.

Moving a number off a trunk and onto a Bin is two changes, and the order matters:
**take it off the trunk first, then set the Bin.** Setting the Bin while the
number is still on a trunk does not move it, and that failure is silent. Read
where a number points with the command in step 4 rather than wondering which
agent answered. If two of your agents share a greeting and a voice they are
indistinguishable by ear.

### When something does not work

| What happens | Where to look |
|---|---|
| A working agent answers, but not this one | The number is still on a SIP trunk or an old webhook, so your markup was never consulted. Read the number's configuration with the command above. |
| The call connects, the spoken line plays, then silence and a drop | Either the service host in your markup is wrong or this agent is not `ready`. **The tell is that this agent's log is empty**: a connection the platform refuses never reaches it. Check `pipecat cloud agent status salon-concierge-v2-pipecat` first. Then re-read the service host: the agent name is `salon-concierge-v2-pipecat`, and the organization is the **slug** from `pipecat cloud organizations list`, the one shaped like `three-random-words-12345`, never your display name (carrier action 2). Twilio's own call log names the stream error outright, under Monitor, Logs, Calls. |
| The spoken line plays, a long silence, then the agent | A **cold start**, and it is the single biggest thing you can fix. With no warm instance the platform has to start a container after the call arrives: schedule it, import pipecat, load the voice-activity and end-of-turn models. That is the silence, and it happens on the first call after every idle period, not just after a deploy. This deployment already holds 1 instance ready, so if you are reading this row the pool is not the cause: check `pipecat cloud agent status salon-concierge-v2-pipecat` for a deploy that has not gone ready. Your agent's own log tells you which half you are looking at: `[pcc-observability] First bot speech | latency=...` measures only the part inside the process, so if that says two seconds and the caller waited twelve, the other ten were the container starting. |
| The spoken line plays and the agent never arrives at all | The same cold start, past the point where the session gives up. A session that expires before the container is listening leaves an **empty session**: `pipecat cloud agent sessions <name>` shows it as `Complete` with a blank `Bot Start Seconds`, and the agent log has no `pipecat.workers.runner` line for it, because `bot()` was never called. Measured 2026-08-26: a session created at T+0 ended at T+10 while the server bound at T+12. So a warm instance is not only a latency nicety here, it is what makes the call answerable. This project has a knowledge base, which makes it much easier to hit: the corpus is embedded at import, before the server binds, so read the "Baking the index" section above. |
| Silence or degraded audio, everything else correct | Your markup names a different region than this deployment. The address in this file is the right one; re-paste it. |
| Nothing in the agent log at all | The call never reached the platform. That is the first row, or the number is not the one you think. |
| The transfer never rings the destination | The per-call check refuses a missing destination by name at session start, so if the session started, read the agent log for the update request's response. |


## Trace with Langfuse

Tracing is configured. Set `LANGFUSE_SECRET_KEY`, `LANGFUSE_PUBLIC_KEY`, and
`LANGFUSE_BASE_URL` in `.env`; all three are required. The trace name is
`concierge-salon-concierge-v2-pipecat`; one trace contains the full conversation. The
Pipecat runner session ID is also the Pipecat conversation ID and Langfuse
session ID.
Pipecat tracing owns the process OpenTelemetry provider and startup fails if another SDK provider is installed first.


Starting the worker or exporting a synthetic span proves connectivity only.
Complete at least one user turn before reviewing the trace. A traced request
contains `conversation` and `turn` spans plus `llm` and `tts` generation
observations.

Traces can contain caller speech, model input and output, and tool arguments and results.
Use only fake identities and fake customer data for release tests. Use a separate
Langfuse project for those tests, and do not send real customer data until its
access and retention rules are approved.


## Carrier setup you have to do yourself

Going live on this route needs real carrier configuration. Every step below is
dictated by the route and none of them is done for you. Until they are done, the
number does not reach this agent.

1. select a Voice-capable number you own in this Twilio account (or reuse the one another target already uses; a number serves one target at a time), and take it off any SIP trunk first, because a number on a trunk ignores its voice configuration silently
1. run `pipecat cloud organizations list` and note the organization name; it is the one value the compiler cannot know
1. create a TwiML Bin holding the exact markup the generated README dictates, with the organization name pasted in (Twilio Console, TwiML Bins, create)
1. point the number's "A call comes in" at that TwiML Bin (Phone Numbers, Manage, Active Numbers, your number, Voice Configuration)

## Agents

- `complaint_specialist`
- `concierge`
Entry agent: `concierge`.
