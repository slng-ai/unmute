# Contract: the README "Telephony setup" runbook

The generated README of a LiveKit **SIP** package MUST contain one section named `## Telephony setup`, replacing today's `## Configure self-hosted LiveKit SIP` heading and its scattered instructions. This contract fixes its structure and the claims it may make. Sources: spec table V1 to V8, all read 2026-08-12.

**Scope**: the SIP branch only. The `livekit/connector/twilio` route carries the inbound feature but has no SIP trunk, no inbound trunk record, and no provisioning script; its own connector section stays as it is and MUST NOT gain this runbook.

**Hosting**: the runbook is written for LiveKit Cloud (research D5b). Exactly one step differs for a self-hosted deployment, the origination target, and that difference appears as a short labelled note beside step 4, never as a second runbook.

## Shape

1. **Opening paragraph**: states the whole cost up front: N carrier actions (six for Twilio with a cold transfer, five without) and one command, and that the carrier part is the operator's because we cannot log into their carrier.
2. **Prerequisites line**: `lk` (authenticated against the project) and `jq`. Nothing else. `envsubst` is no longer mentioned anywhere.
3. **Part one, carrier**: `### At your carrier (Twilio)` when the route's carrier is `twilio`; a generic `### At your carrier` fallback otherwise (see below).
4. **Part two, LiveKit**: `### At LiveKit`, carrier-neutral, identical wording for every carrier.
5. **Transfer notes** (all four are pinned by test, FR-007): cold transfer needs nothing at LiveKit beyond inbound; the carrier toggles are its only setting; it cannot be tested from the Agent Console, because there is no phone leg to refer; and the `human transfer failed after Ns` log line maps to the missing Call Transfer or PSTN transfer toggle from step 6.
6. **Retirement sentence**: exactly one sentence stating that earlier builds used `LIVEKIT_SIP_INBOUND_TRUNK` and that the variable is retired and safe to delete from `.env` and platform secrets. This is the only permitted occurrence of the name anywhere in the build.

## Part one for Twilio (each step is a console path or a command, in this order)

| # | Step | Must state |
|---|---|---|
| 1 | Buy a voice-capable number | Twilio console, phone numbers; skip if owned |
| 2 | Create an Elastic SIP trunk | Elastic SIP Trunking, Manage, Trunks, Create |
| 3 | Termination (dial-out) | on the Termination tab, set the Termination SIP URI: one domain name ending in `pstn.twilio.com`. **This single value is the hostname env** (`SIP_TRUNK_HOSTNAME` in the example); the runbook must say that once and not describe the trunk domain and the termination URI as if they were two values. Then create or select a Credential List whose username and password are the values for the username and password envs |
| 4 | Origination (inbound) | add an Origination URI set to the project SIP URI with `;transport=tcp`, priority and weight 1, enabled; dictate both ways to get it: the LiveKit Cloud project settings page, or the runnable derivation in the `### Get your origination URI` section above the carrier branches. MUST warn against building it from `LIVEKIT_URL`: the project URL subdomain and the SIP subdomain are different strings (verified 2026-08-12 on a real project, `wss://slng-atlas-6sw2n5o2.livekit.cloud` against project ID `p_68xpbc5hlhk`), so the obvious guess is wrong and fails as a call that rings nowhere. Labelled note for self-hosted deployments: there is no project SIP URI, so origination points at your own deployed LiveKit SIP endpoint (the public SIP signaling address from the deploy step) |
| 5 | Attach the number | trunk's Numbers tab (or `twilio api trunking v1 trunks phone-numbers create`); the number is the value for the from-number env |
| 6 | Transfer toggles (only when the package has a cold transfer) | Features: Call Transfer (SIP REFER) Enabled, Enable PSTN Transfer ticked, pick a Caller ID for Transfer Target; equivalent CLI: `twilio api trunking v1 trunks update --sid <trunk-sid> --transfer-mode enable-all --transfer-caller-id from-transferee`; note that transfer caller ID is a trunk setting, never per call |

## Part one, the runnable half

One carrier-neutral `### Get your origination URI` section sits above both carrier branches, because both need the same value and the Twilio command block below it consumes the variable it sets. Its primary command lists **every** configured project with its SIP URI, one row each: name, URL, and `sip:<ProjectId without p_>.sip.livekit.cloud;transport=tcp`.

It MUST NOT derive the value from `LIVEKIT_URL`, and MUST say why not: an emitted first draft matched `.env`'s `LIVEKIT_URL` against the `lk` config and printed nothing on the shipped example, because a Cloud `.env` carries no `LIVEKIT_URL` at all (the platform injects it). Listing every project instead cannot fail that way, and putting the two columns side by side shows the reader that the URL subdomain and the SIP subdomain are unrelated strings. It MUST also say to keep the output piped through `jq`, because the raw listing contains the project API key and secret.

The Twilio branch additionally carries `#### The same steps as commands` and `#### Check the carrier side`. Rules for that block:

- Every command is a real command of the installed CLIs, with its version and verification date stated in the prose above it (twilio-cli 6.2.4, `lk` 2.18.2, 2026-08-12), and every one was run or `--help`-verified rather than recalled. No invented flags, no guessed enum spellings: `transfer-mode enable-all` and `transfer-caller-id from-transferee` were confirmed against live trunk records, which report the same lowercase-hyphenated forms.
- No SID is ever transcribed. The trunk is found by the domain the operator chose, the credential list by its friendly name, the number by its own digits. This is the same resolve-by-what-you-know rule the provisioning script follows.
- The SIP password is read at a prompt (`read -rsp`), never written into the block, a file, or shell history, and no command echoes it.
- The check block prints exactly the three states that break this side: a number attached to no trunk, a transfer mode that forbids PSTN transfer, and a missing or disabled origination URI. Each line says what its bad value means.
- The block stays optional: the numbered console steps above it remain complete on their own, because an operator without the Twilio CLI must not be blocked.

## Part one fallback (carrier without a dedicated block)

Names the same obligations without console paths: an origination target set to the project SIP URI, a termination address with username and password auth matching the three envs, the number attached to the trunk, and transfer enablement if the package has a cold transfer. Points at the route's provider docs URL for the carrier's own paths.

## Part two, LiveKit (carrier-neutral)

- One command: `bash telephony-setup.sh`, run from the build directory with `.env` filled.
- States what it does in one sentence each: claims your number for this project (inbound trunk), routes its calls to this agent (dispatch rule), finds everything by your phone number, safe to run again.
- States the two failure meanings: number already claimed by a trunk this project cannot see (someone else owns it), and `lk` not authenticated.
- MUST NOT name any carrier, any trunk ID, or any step that reads output from one command into another.

## Forbidden content

- No secret value, no example credential, no phone number other than the operator's own env names.
- No em or en dashes.
- No step that is only a link. Links may follow a dictated action, never replace one.
- No claim without a source and date where the claim is about platform behavior.
