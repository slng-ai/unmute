# Contract: the runbook (the README's telephony section)

**Feature**: [../spec.md](../spec.md) | **Date**: 2026-08-13

What the generated README's telephony section must contain for this route, in
order, with the counts stated up front. The section opens by saying what is
about to be true: **nothing runs on your side and nothing is hosted by you**;
the whole setup is actions in the Twilio console.

## Part zero: who does what

Two parties, not three, and no operator-run piece:

- **Twilio** owns the number and holds the small piece of call markup that sends
  a call's audio to the platform.
- **Pipecat Cloud** runs the agent and receives the audio directly.

The section states the count: at most four steps for inbound, three of them in
the console and one a lookup command; nothing at all to run in production.

## Part one: at your carrier (Twilio)

1. **A voice-capable number you own in this Twilio account** (skip if owned; the
   note about reusing the account from other targets in this repository carries
   over from 006's runbook). Where the package also places calls, the runbook
   states that this same number can be the outbound caller identity, that voice
   capability is the whole requirement on the number, and that what can still
   refuse a call sits on the account rather than the number (spec FR-006a).
2. **The organization lookup**, the one value the compiler cannot know:
   `pipecat cloud organizations list`. One command, run anywhere.
3. **Create the TwiML Bin** with the exact markup from
   [carrier-markup.md](carrier-markup.md) §1, agent name already rendered,
   organization the one paste. Console path dictated (TwiML Bins → create).
4. **Point the number at the Bin**: Phone Numbers → Manage → Active Numbers →
   the number → Voice Configuration → "A call comes in" → TwiML Bin. The
   runbook repeats 006's hard-won rule here: a number still attached to a SIP
   trunk ignores this setting silently. Take it off the trunk first, and read
   the state with `twilio api core incoming-phone-numbers list --properties
   phoneNumber,trunkSid,voiceUrl` instead of listening.

Renumbering discipline as in 006: actions that exist only for some declarations
renumber without gaps.

## Part two: on this side

**Nothing, in production.** The section says so in one line, because the
absence is the feature. Deploy per the README's deploy section, wait for
`ready`, done. No helper, no second terminal. (A package with outbound or
transfer additionally fills the four environment names per
[environment.md](environment.md) before deploying the secret set.)

## Part two and a half: hear it locally first (optional)

One command, before any deploy: `unmute dev --telephony` runs the agent
locally in the carrier's stream mode, opens a cloudflared tunnel, points the
declared number at it for the session, and puts the number back on exit. The
runner answers the carrier's webhook itself locally, so no markup is created
and the production Bin is never touched (research F12, D11). This subsection is
the **only** place a tunnel is mentioned, it names cloudflared and never ngrok,
and it links the grounding sources (spec FR-003): the Pipecat `twilio-chatbot`
example, Twilio's Media Streams protocol page, and the TwiML Bins guide.

## Part three: place an outbound call (only when declared)

One command against Twilio's API, TwiML inline
([carrier-markup.md](carrier-markup.md) §2), `From` and organization read from
the environment, destination typed. Expected result stated: the phone rings
showing the operator's number; the agent greets on answer.

## Part four: every step, in order, from a source change to a phone call

The 006 pattern, shorter because there is less: recompile from the source
package (with the overwrite warning) → cd → fill `.env` (only if the shape
requires one) → `secrets set` (ditto) → `deploy` + wait for `ready` → do the
carrier part once → test inbound → test outbound (if declared) → test the
transfer both ways (if declared), the decline drill named as the run that
matters → where the logs are (`pipecat cloud agent logs`; there is no second log
on this route).

## Part five: security note

Stated plainly, from research F7/F8: this route's deploy sets
`websocket_auth = "none"` because a static Bin cannot fetch a token; what limits
who can start a session is knowledge of the `AGENT.ORGANIZATION` string. Treat
that string like a capability: it is not a secret in the cryptographic sense,
but anyone holding it can open sessions the operator pays for. This is the
platform's documented model for direct carrier connections.

## Part six: transfer honesty

Where a cold transfer is declared, the runbook states, in operator words, the two
limits whose canonical specification is [carrier-markup.md](carrier-markup.md)
§3: a failed transfer
brings back a fresh agent that does not remember the call, and a completed
transfer that ends by the other side hanging up does the same. The route
comparison in the docs is linked as "if you need the same agent to survive a
failed transfer, use the Daily carrier route".

## Part seven: when something does not work

The troubleshooting map, one row per named cause:

| What happens | Where to look |
|---|---|
| A working agent answers, but not this one | the number is on a trunk or an old webhook; read the number's config (the command above); the Bin was never consulted |
| The call connects, the spoken line plays, then silence and a drop | the service host in the Bin is wrong (agent or organization misspelled), or the agent is not `ready`; check `pipecat cloud agent status` first, then the Bin values |
| The spoken line plays, long silence, then the agent | cold start; keep a warm instance (`--min-agents 1`) or keep the spoken line |
| Wrong-region silence or degraded audio | the Bin's wss URL names a different region than the deployment; the README's rendered URL is the correct one, so re-paste it |
| Outbound command errors | printed in Twilio's own words; nothing is swallowed. The two account-level causes to check are the destination country's permissions and any restriction on calling unverified destinations, neither of which is a property of your number |
| Transfer never rings the destination | the per-call check would have refused a missing destination by name at session start; if it started, read the agent log for the update request's response |

## Contract tests (offline)

The runbook contract test asserts: the counts are stated and match the numbered
list that follows them; the caller-identity definition appears wherever the
number is asked for; the words "tunnel",
"ngrok", "cloudflared", and "helper" do not appear in the **production** parts
(everything outside the local development subsection), and "ngrok" and "helper"
appear nowhere at all; the local development subsection names cloudflared and
the number restore; the Bin markup, the lookup command, and the trunk warning
are present for inbound packages; the three grounding source links are present;
the outbound command appears exactly when declared; the security note names
`websocket_auth` and the capability sentence; the fresh-session transfer limit
appears when a transfer is declared; and no secret-looking literal appears
anywhere in the section.
