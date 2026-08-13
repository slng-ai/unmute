# Contract: the README "Telephony setup" runbook

**Feature**: [../spec.md](../spec.md) | **Date**: 2026-08-12

The generated README for a build on `(pipecat, daily-sip, twilio)` carries one ordered section headed "Telephony setup". This contract is what the template must render and what the contract test asserts. The no-carrier Daily build renders none of it and keeps its current README bytes.

## Placement and shape

- Rendered only when the target declares a carrier. Appears after the deploy section, because deploy and secrets must exist before a call can land.
- Opens by stating the cost up front: how many carrier actions and how many commands here, with the numbers computed from what the package declares (spec SC-003 caps: at most six carrier actions, at most two platform commands).
- Two labeled parts, in this order: "At your carrier (Twilio)", then "On this side". The platform part names no carrier and would read correctly for any SIP-capable carrier (spec US4).
- No step transcribes an identifier from one step into another. No step is a bare link; every step is an exact console path or a copy-paste command.

## Part one: at your carrier (Twilio)

Dictated actions, in order. Actions 3 and 4 render only when the package declares outbound or a cold transfer.

1. **A voice-capable number.** Buy one, or reuse the one you have. Operators who completed the LiveKit setup from specs/005 are told here that they can reuse the same account and number, and which of the following they already own.
2. **Point the number at the helper.** Console path to the number's voice configuration, set "A call comes in" to a webhook, method POST, at `https://<your helper host>/call`. The runbook states that this is the same URL the tunnel command in part two prints.
3. **A trunk termination address for outbound.** Console path to Elastic SIP Trunking, create or reuse a trunk, note the termination address; it is the value of the `sip_address` env name. Stated plainly: pick a termination prefix nobody can guess, because action 4 authenticates by address list, not by password.
4. **Allow Daily's addresses.** Create an IP access control list containing the `sip.hosts` entries from `https://ip-info.daily.co/ips/ip-info.json` and attach it to the trunk's termination. State that the list is dynamic, that changes are published in the same file three days ahead, and that a termination that starts rejecting calls means re-checking this list.

Forbidden content: credential lists for termination (nothing on the Daily side could hold the credentials, research F3), any SIP REFER or PSTN transfer toggle (this route's transfer keeps Daily anchored and sends no REFER to the carrier, research F4).

## Part two: on this side

At most two commands, both copy-paste, both reading every value from the filled `.env`:

1. Run the helper from the build directory.
2. Expose it with a tunnel command, whose printed URL is what action 2 above takes.

The part states what the helper is for in one sentence, that it must be reachable whenever calls should land, and that production hosting of it (public ingress, TLS) belongs to the operator.

## Accompanying notes the section must carry

- **One number, one target.** A number serves one target at a time. Moving it from the LiveKit target to this one is changing the number's voice configuration from the trunk to the webhook; moving back is the reverse. Both directions stated (spec FR-015).
- **Transfer cost.** After a completed cold transfer, Daily stays in the call path and both legs keep billing until the call ends (research F4, R8).
- **Caller identity on outbound.** Governed at the carrier; what the recipient's display shows is recorded as unverified until the live run (research F2).
- **Account prerequisite.** The Daily dial-out approval, already rendered by the existing prerequisites section; the runbook points at it rather than repeating it.
- **Troubleshooting map.** What the caller hears, mapped to the cause: carrier error tone or announcement means the webhook URL is wrong or the helper is unreachable; eternal hold means the agent never joined or the forward failed, and where to read the helper's log line naming the cause; outbound call never rings means the allow-list or the termination address, in that order.

## Contract test

An L1 test over the rendered README asserts: the section exists exactly once for a carrier build and zero times for a no-carrier build; the stated counts match the rendered actions; the platform part contains no carrier name; the forbidden content is absent; every env reference is a name, never a value; and a second-carrier fixture renders generic carrier prose without touching the platform part (mirrors the specs/005 contract tests).
