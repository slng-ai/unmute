# Research: Pipecat native WebSocket telephony

**Feature**: [spec.md](spec.md) | **Date**: 2026-08-13

Every platform fact below was read on 2026-08-13 from one of two sources, never
from model memory: the local checkout of the Pipecat documentation repository
(`../pipecat-docs`, cited by file path) or the installed `pipecat-ai==1.5.0`
package source (cited by module path). Facts are F-numbered; decisions are
D-numbered and cite the facts they rest on.

## Facts (dated 2026-08-13)

**F1. The platform natively terminates Twilio Media Streams.** Pipecat Cloud
ships a dedicated endpoint per carrier: `wss://api.pipecat.daily.co/ws/twilio`,
regional form `wss://{region}.api.pipecat.daily.co/ws/twilio` (example given:
`eu-central`). Source: `pipecat-cloud/guides/telephony/twilio-websocket.mdx`,
`pipecat-cloud/guides/regions.mdx` ("Regional WebSocket Endpoints").

**F2. The agent is named inside the TwiML.** The documented Bin is:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<Response>
  <Connect>
    <Stream url="wss://api.pipecat.daily.co/ws/twilio">
      <Parameter name="_pipecatCloudServiceHost"
         value="AGENT_NAME.ORGANIZATION_NAME"/>
    </Stream>
  </Connect>
</Response>
```

The organization name comes from `pipecat cloud organizations list`. Source:
`pipecat-cloud/guides/telephony/twilio-websocket.mdx`. The telephony overview
(`pipecat-cloud/guides/telephony/overview.mdx`) also documents a query-parameter
form, `.../ws/twilio?serviceHost={agentName}.{organizationName}`; both are
current. The Bin `<Parameter>` form is the one the Twilio-specific guide
dictates, so it is the one this feature emits.

**F3. Custom TwiML parameters reach the bot.** All WebSocket messages are
forwarded to the bot, "including any custom parameters set in your TwiML".
Twilio's start message carries `customParameters`, which pipecat surfaces as
`call_data["body"]`; `callSid` and `streamSid` are surfaced as
`call_data["call_id"]` and `call_data["stream_id"]`. Caller and callee numbers
are populated only from custom parameters named `from_number` and `to_number`.
Sources: `pipecat-cloud/guides/telephony/twilio-websocket.mdx` ("How It Works");
`pipecat/runner/utils.py::parse_telephony_websocket` in the installed 1.5.0.

**F4. The runner builds the transport itself.** For a WebSocket session,
`create_transport` calls `parse_telephony_websocket`, detects `twilio`, reads
`transport_params["twilio"]` (a `FastAPIWebsocketParams` factory), forces
`add_wav_header = False`, attaches a `TwilioFrameSerializer` constructed with the
stream and call ids plus `TWILIO_ACCOUNT_SID` / `TWILIO_AUTH_TOKEN` **from the
environment, defaulting to empty strings**, and sets `runner_args.transport_type`
and `runner_args.call_data` so the bot can read the parsed handshake without
re-parsing. Source: `pipecat/runner/utils.py::create_transport`,
`::_create_telephony_transport` in the installed 1.5.0. Consequence, as first
written: a pure-inbound package needs no Twilio credentials; the serializer's
REST-based call control is simply inactive when they are absent. **Corrected by
F15**, which found by running the image that the serializer *refuses to be built*
without them rather than going quiet. The rest of F4 holds.

**F5. Call control is TwiML update by CallSid.** "The call session is controlled
by updating the TwiML associated with each call's unique identifier
(`CallSid`)"; the platform doc names this as the mechanism for "transferring to
human agents". This is the same one-request pattern feature 006 already emits
(`POST /2010-04-01/Accounts/{sid}/Calls/{callSid}.json` with a `Twiml` form
field). Sources: `pipecat-cloud/guides/telephony/twilio-websocket.mdx`;
`pipecat/telephony/twilio-websockets.mdx`.

**F6. Dial-out is one REST request with inline TwiML.** The documented dial-out
creates the call via Twilio's REST API with TwiML that `<Connect><Stream>`s back
to the endpoint, custom `<Parameter>` elements included (the doc's own example
passes `call_type=outbound`). The stream, and therefore the bot, starts when the
callee answers. Source: `pipecat/telephony/twilio-websockets.mdx` ("Dial-out").

**F7. `websocket_auth` exists, and the docs disagree on its default.**
`pipecat-cloud/guides/websocket-authentication.mdx` and
`pipecat-cloud/fundamentals/deploy.mdx` both state the default is `"none"`;
`pipecat-cloud/security/security-and-compliance.mdx` states "New agents default
to requiring token authentication. For telephony providers that connect directly
(Twilio, Plivo, etc.), you can set `websocket_auth = "none"`". The token flow
requires the client to call `/start` first, which a static TwiML Bin cannot do.
Consequence: direct carrier connection requires `websocket_auth = "none"` in
practice, and relying on any default is relying on a fact the platform's own
docs state two ways.

**F8. What guards the endpoint at `"none"` is the service host string.** With
`websocket_auth = "none"`, the connection is accepted on the strength of naming
`AGENT_NAME.ORGANIZATION_NAME`. The platform applies protocol validation per
carrier endpoint ("each with protocol validation for added reliability",
`overview.mdx`), but no caller secret is involved. Anyone holding the service
host string can open a session the operator pays for. This is the platform's
documented model for direct-connect telephony (F7), not something this feature
invents; the emitted README must state it plainly (spec, Assumptions).

**F9. The platform also terminates Telnyx, Plivo, and Exotel streams.** Source:
`pipecat-cloud/guides/telephony/overview.mdx`. Out of scope here (spec
Assumptions), but the route naming must leave room (D1).

**F10. The `websocket` extra of pipecat-ai 1.5.0 carries fastapi**, which the
`runner` extra also carries. Source: `pipecat_ai-1.5.0.dist-info/METADATA`
(`Provides-Extra: websocket`, `Requires-Dist: fastapi<1,>=0.115.6; extra ==
"websocket"`).

**F12. The runner has a built-in local Twilio mode.** `python bot.py -t twilio
-x <public-host>` makes the local dev runner serve the carrier webhook itself,
answering with `<Response><Connect><Stream url="wss://{host}/ws">` TwiML and
terminating the stream on its own `/ws`. No TwiML Bin exists locally. Source:
`pipecat/runner/run.py` in the installed 1.5.0 (usage line "Telephony:
`python bot.py -t twilio -x your_username.ngrok.io`"; the TwiML literal at the
`--proxy` handler). Verified 2026-08-13.

Exact spellings, re-read 2026-08-13 for T003 (`pipecat/runner/run.py`, the
`argparse` block and the `--proxy` handler):

| Piece | Spelling |
|---|---|
| transport flag | `-t` / `--transport`, value `twilio` (`TELEPHONY_TRANSPORTS = ["twilio", "telnyx", "plivo", "exotel"]`) |
| proxy flag | `-x` / `--proxy`, **hostname only**; a `http://` or `https://` prefix is stripped with a warning and a trailing `/` removed (`_validate_and_clean_proxy`) |
| host / port | `--host` (default `RUNNER_HOST`), `--port` (default `RUNNER_PORT`, 7860) |
| webhook the runner answers | `POST /`, replying `<?xml ...?><Response><Connect><Stream url="wss://{proxy}/ws"></Stream></Connect></Response>` |
| stream path | `/ws` (and `/ws/{token}`) |

Consequence for the dev flow: the local webhook path is `/`, not any path this
repository invents, and the tunnel must point at the runner's port.

**F11. Feature 006's live run proved the deploy shape this route inherits.** The
Pipecat Cloud image must be built on `dailyco/pipecat-base` (the platform starts
sessions by POSTing `/bot` to the container); that fix landed in the shared
Dockerfile template on 2026-08-13 and this route rides it unchanged. Source:
this repository, `specs/006-pipecat-carrier-telephony/tasks.md` ("The first live
attempt").

**F13. The three grounding sources, checked 2026-08-13.** All three answer 200
and are the ones the emitted README and the docs link (spec FR-003):

| Source | Title | URL |
|---|---|---|
| The working example | Twilio Chatbot (pipecat-examples) | <https://github.com/pipecat-ai/pipecat-examples/tree/main/twilio-chatbot> |
| The wire protocol | Media Streams WebSocket Messages | <https://www.twilio.com/docs/voice/media-streams/websocket-messages> |
| Where the markup lives | Getting started with TwiML Bins | <https://help.twilio.com/articles/360043489573-Getting-started-with-TwiML-Bins> |

**F14. The outbound caller identity, now verified in full.** Closed 2026-08-13
(T002a) against Twilio's own documentation. Four facts, each with its page:

1. **`From` is the caller identity and must be one of two things.** "The 'To' and
   'From' parameters are required. The `From` parameter must be a Twilio number
   or a verified outgoing caller ID. If the destination is a phone number, the
   caller ID must also be a phone number." Source:
   `twilio.com/docs/voice/api/call-resource`.
2. **A verified outgoing caller id is a real alternative**, created by a
   validation request against a number the operator can answer
   (`OutgoingCallerIds`, `ValidationRequest`). Source:
   `twilio.com/docs/voice/api/outgoing-caller-ids`. Consequence for this
   feature: "a number you own in this account" is the simple case the runbook
   dictates, and it is not the only legal value, so the runbook says "own in
   this account **or** verified on it" rather than overstating.
3. **Destination-country permissions are an account-level setting**, per
   country, with three separately toggled risk classes: low-risk numbers,
   high-risk special service numbers, and high-risk toll-fraud numbers
   (`GET voice.twilio.com/v1/DialingPermissions/Countries`). Source:
   `twilio.com/docs/voice/api/dialingpermissions-country-resource`. This is a
   property of the account, never of the number.
4. **A trial account can only call verified destinations**, and the errors say
   so by number: **21219** "'To' phone number not verified", **14111** "Invalid
   To phone number for Trial mode", and **32100** for the SIP form. The fix is
   verifying the recipient or upgrading the account. Sources:
   `twilio.com/docs/api/errors/21219`, `/14111`, `/32100`.

Nothing about `From` is left unverified now, so the three FR-006a claims are
free to appear in emitted text, and the troubleshooting map names causes 3 and 4
as the two account-level places to look.

**F15. The framework's telephony transport refuses to be built without carrier
credentials, and F4 was incomplete about it.** Found by running the emitted image
on 2026-08-13, not by reading: `create_transport` constructs
`TwilioFrameSerializer(..., account_sid=os.getenv("TWILIO_ACCOUNT_SID", ""),
auth_token=os.getenv("TWILIO_AUTH_TOKEN", ""))`, and that serializer's
`__init__` **raises** when its `auto_hang_up` parameter is true (the default) and
either credential is falsy: `auto_hang_up is enabled but missing required
parameters: account_sid, auth_token`. Sources:
`pipecat/runner/utils.py::_create_telephony_transport` and
`pipecat/serializers/twilio.py::TwilioFrameSerializer.__init__` in the installed
1.5.0; observed as a live session failure in the emitted container.

F4's consequence ("a pure-inbound package needs no Twilio credentials; the
serializer's REST-based call control is simply inactive when they are absent")
was therefore wrong in one word: inactive, no. Refused. Upstream's own guide for
this route lists both names in its environment setup unconditionally
(`pipecat/telephony/twilio-websockets.mdx`), which is consistent with that.

*Consequence for this feature*: spec FR-005's promise is kept, and it is kept by
emitting the transport rather than by asking the framework for it. On the
**no-connection** shape only, the emitted bot builds
`FastAPIWebsocketTransport` itself with
`TwilioFrameSerializer.InputParams(auto_hang_up=False)`, which is exactly the
behaviour FR-005 specifies for that shape: the call ends when the stream closes,
because the markup has nothing after `<Connect>`. On every other shape the
framework's path is used unchanged. Proven both ways against the built image on
2026-08-13 by driving a synthetic Twilio handshake into the container: both reach
`phone call CA... (inbound)` and activate the entry agent.

## Decisions

**D1. Route key: `(pipecat, "cloud-websocket", "twilio")`.**
*Rationale*: the transport names where the websocket terminates (Pipecat Cloud),
which is exactly what distinguishes it from the self-hosted `carrier-websocket`
transport; the carrier stays a separate axis, so Telnyx or Plivo later is a new
row, not a new name (F9). *Alternatives considered*: `native-websocket` (whose
native?), `platform-websocket` (the repo says "Pipecat Cloud", not "platform",
everywhere user-facing), overloading `carrier-websocket` with a mode flag
(rejected: two shapes under one name is how silent downgrades start).

**D2. The Bin is dictated, not emitted as a file.** The README renders the exact
TwiML with two placeholders (agent name is compiled in; organization name and,
when declared, region are substituted), plus a short spoken line before
`<Connect>` so a cold start is never dead air. *Rationale*: the Bin lives in
Twilio's console; an emitted `.xml` file would be a second copy that drifts
(constitution III). The spoken line costs about a second on every call and
covers the boot window; the README notes it may be deleted once a warm instance
is kept. *Alternatives*: no spoken line (the platform guide's own Bin; rejected
because SC-002 requires the caller hear something within 2 seconds and a cold
start is longer than that); hold music URL (rejected: third-party asset rot,
same reasoning as 006).

**D3. Manifest gains `websocket_auth = "none"`, explicitly, on this route
only.** *Rationale*: F7. The docs state the default both ways, and a static Bin
cannot fetch a token, so the working value is `"none"`; emitting it makes the
security posture a visible, versioned choice instead of an inherited default.
The README's security note states F8 in plain words: the service host string
works like a capability; treat it accordingly. *Alternatives*: rely on the
default (rejected: F7's contradiction makes that a coin flip); token auth
(rejected: structurally impossible from a static Bin).

**D4. Connection vocabulary: `account_sid`, `auth_token`, `from_number`; and a
pure-inbound package needs no connection at all.** `sip_address`,
`sip_username`, `sip_password` are refused by name: there is no SIP anything on
this route. *Rationale*: F4 (inbound needs no credentials), F5/F6 (outbound and
transfer are REST calls needing the account pair, and outbound needs a caller
id). The mutual-requirement guards mirror 006's: outbound or cold transfer
declared without a connection fails naming all three fields; a connection with
no phone channel fails as dead weight.

**D5. One new environment name: `PIPECAT_CLOUD_ORGANIZATION`.** Required exactly
when the package places or redirects calls (outbound or cold transfer), because
those compose TwiML that must name the service host (F2, F6, D7), and the
compiler knows the agent name but cannot know the organization. *Rationale*: the
alternative, the operator pasting the full host into every command, retypes a
value that never changes; an env name is read once. Checked per phone session
alongside the connection values (spec FR-008); browser and console sessions
never see it.

**D6. Outbound is one curl to Twilio, not to the platform.** The README's
command creates the call with inline TwiML connecting the stream to the
platform, `direction=outbound` as a custom parameter, `From` read from the
connection's `from_number` env, `To` typed by the operator (F6). The bot reads
`runner_args.call_data` (F3, F4) and greets on connect, which fires on answer,
so the greeting meets a human, not ringback. *Alternatives*: the platform
`/start` endpoint (rejected: it has no way to place a carrier call on this
route; the call must originate at Twilio).

**D7. Cold transfer: update the call's TwiML, announcement included; the failure
path reconnects a fresh session and says so.** Amended 2026-08-13 during
implementation: the announcement is the update's first verb rather than a line
the agent speaks, because applying the update tears the stream down and would cut
the agent off mid-word, and pipecat 1.5.0 exposes no speech-finished event to
wait on. The reasoning is recorded once, in
[contracts/carrier-markup.md](contracts/carrier-markup.md) §3. The emitted TwiML
is a leading `<Say>`, then
`<Dial answerOnBridge="true" timeout="25">destination</Dial>` followed by a
spoken failure line and a `<Connect><Stream>` naming the same service host
(composed from the compiled agent name, `PIPECAT_CLOUD_ORGANIZATION`, and the
compiled region). *Rationale*: F5 is the platform's own transfer mechanism.
Branching on the dial outcome requires an `action` callback URL, which is a
hosted endpoint, which this route exists to not have. So failure handling is
sequential TwiML: if the dial ends without connecting (decline, timeout), Twilio
speaks the line and reconnects the caller to the agent. *Known and stated
limits*: both are specified once, in
[contracts/carrier-markup.md](contracts/carrier-markup.md) §3, which is their
canonical home; everything else references it rather than restating it, so the
wording cannot drift. In short: the reconnected session is fresh, and the same
continuation runs when a completed transfer ends by the destination hanging up
first. *Alternatives*: `action` pointing
at a second TwiML Bin (rejected: Bins are static; they cannot branch on
`DialCallStatus`); conference bridging (rejected: more legs, same continuity
problem, and cost on every leg); dropping the failure requirement (rejected:
spec FR-007 and the 006 precedent both demand the caller never be stranded
silently).

**D8. The bot's session detection reads `runner_args.call_data` after
`create_transport`, not a start body.** On this route there is no `/start` body;
the handshake is the source of truth and pipecat exposes it precisely so bots do
not re-parse it (F4). A session with `transport_type == "twilio"` is a phone
call; `direction` in `call_data["body"]` distinguishes outbound; everything else
(browser, console) behaves exactly as on a package with no telephony.
*Consequence*: the phone-session environment check (connection values,
`PIPECAT_CLOUD_ORGANIZATION`, transfer destination) runs at that point, before
the conversation starts, by name.

**D9. The dictated Bin passes `from_number`/`to_number` custom parameters using
Twilio's Bin templating (`{{From}}`, `{{To}}`)** so `call_data`'s typed fields
are populated (F3). No `source.*` call-source binding is granted on this route
in v1, matching the 006 route's posture; the refusal message already names where
that works.

**D10. Capabilities and evidence.** The row grants: route selection, inbound,
outbound, cold transfer, hangup, every one `provisional` with the note "built
and offline-proven; no call has been placed through this endpoint yet" until the
live half of quickstart.md is run and dated. Warm transfer stays refused with a
message naming what this route would need. Hangup rides the serializer's
end-of-pipeline call control when credentials exist (F4) and socket close (which
ends the call, since the Bin has no TwiML after `<Connect>`) when they do not.

**D11. `unmute dev --telephony` runs the phone path locally, end to end.**
Clarified 2026-08-13 (spec, Clarifications): one command compiles, starts the
agent locally in Twilio transport mode with the runner's own webhook (F12),
opens a cloudflared tunnel, points the declared number's voice configuration at
it, and restores that configuration on every exit path, interrupt included.
This reuses the existing dev telephony machinery (tunnel management and Twilio
webhook set/restore already exist for the LiveKit flow). The production Bin is
never touched, no markup is created locally, and cloudflared is the only tunnel
named anywhere, local sections only. *Alternatives considered*: a documented
manual flow with a pointing refusal (rejected by the requester: the machinery
exists, so the one-command flow is the right cost); no local phone path
(rejected: hearing the phone path before deploying is dev mode's value).

**D12. pyproject adds the `websocket` extra when this route is selected.**
*Rationale*: F10. fastapi happens to arrive via `runner` today, but the emitted
project should declare what it uses, not inherit it (constitution III's
derived-not-copied, applied to dependency intent).

**D13. Example set and environment naming, from the 2026-08-13 clarification.**
The telephony examples become one per use case: warm+inbound stays
`examples/human-transfer` (LiveKit; no Pipecat route offers warm transfer
today, and the docs say so); cold+inbound on Pipecat over Twilio is a new
example on this route, `examples/human-transfer-cloud-twilio`, **replacing**
`examples/human-transfer-daily-twilio`; inbound+outbound stays
`examples/telephony-hello`, whose Pipecat target moves from `carrier-websocket`
to `cloud-websocket` and which is audited while touched.
`examples/human-transfer-daily` (Daily-provisioned, no carrier) is untouched.
The new example reuses `telephony-hello`'s `twilio_voice` connection verbatim
(`TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, `TWILIO_PHONE_NUMBER`), so one
`.env` drives every Twilio example; the convention scales as `<CARRIER>_*` per
future carrier, `PIPECAT_CLOUD_*` for platform values, generic names for
destinations. Consequence recorded honestly: the 006 route keeps its rows,
goldens, and code but no longer ships a public example, and its open live tasks
run against a fixture instead.
