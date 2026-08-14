# pipecat-human-transfer-daily

A cold transfer on Pipecat over a **Daily-provisioned number**
(`transport: daily-sip`, no `carrier`), using Daily's own transfer primitive: the
bot announces the handoff, calls `transport.sip_call_transfer`, Daily reroutes the
caller's leg, and the bot drops off. Billing answers knowing nothing about the
call.

Nothing of yours is hosted on this route and there is no carrier account to set
up: the number comes from Daily. If you would rather use a Twilio number you
already own, that is [pipecat-human-transfer-twilio](../pipecat-human-transfer-twilio)
on `transport: cloud-websocket`, which also hosts nothing. The route comparison is
in [docs/TELEPHONY.md](../../docs/TELEPHONY.md).

Warm transfer is not here, and the reason is worth stating precisely because this
file used to state it wrongly. Daily **does** document a warm pattern; this
project has not built it yet, because the pattern needs the generated bot to own
the call's audio (a transfer coordinator, a hold-music mixer, a gate per leg).
That is deliberate work rather than a default, tracked as feature 005. Warm
compiles on LiveKit SIP today, in [livekit-human-transfer](../livekit-human-transfer). The
capability map with sources is [docs/TRANSFERS.md](../../docs/TRANSFERS.md).

---

## The whole runbook

Nothing below is optional if you want a real phone call. Steps 1 and 2 are the
slow ones: dial-out is granted by a human at Daily, and a number cannot be given
back for two weeks.

Verified against the providers' documentation on 2026-08-12. Every command is one
you run yourself; `unmute` never touches your Daily or Pipecat Cloud account.

### 0. Two accounts, two different keys

People lose an afternoon to this, so get it straight first.

| Key | Whose | What it does here |
|---|---|---|
| `DAILY_API_KEY` | your Daily developer account | buys and manages phone numbers, and the dial-in handshake the bot performs. The route's own runtime supplies it, so it is not in `secrets:` — you still have to set it wherever the agent runs |
| Pipecat Cloud public key (`pk_...`) | your Pipecat Cloud org | starts agent sessions, which is how outbound calls begin |

And the three you declare, which are the whole `secrets:` block in `agent.yaml`:

| Variable | What it is |
|---|---|
| `OPENAI_API_KEY` | the reasoning model |
| `SLNG_API_KEY` | the listen and speak models |
| `BILLING_PHONE_NUMBER` | the number the cold transfer dials |

There are no carrier credentials, because `connections/daily.yaml` names none:
Daily carries its own calls.

Get the Daily key from the Daily dashboard. Get the Pipecat Cloud key from its
dashboard, or `pipecat cloud organizations keys create`. Authenticate the CLI once
with `pipecat cloud auth login`.

```sh
export DAILY_API_KEY=...
```

Values never go in this package. The package carries env var **names** only.

### 1. Ask Daily to enable dial-out

Do this first, because a person has to approve it and that takes time.

Dial-out is a **paid Daily feature granted on request**. International dial-out is
granted **separately, per domain**. A cold transfer dials its destination, so this
route does not work without it, and `unmute validate` will tell you so.

Ask through [Daily support](https://www.daily.co/contact/sales). If you skip it,
the failure you get later is one of these, and each names its own cause:

| Error | Cause |
|---|---|
| `property 'enable_dialout' cannot be set to that value with your current plan` | the Daily account has no dial-out |
| `International dialout not supported. Contact sales to enable it for your domain` | domestic granted, international not |
| `dial-out not enabled for room. add enable_dialout property to room` | the account is fine, the room was created without it |

### 2. Buy a phone number

A helper is in the repository, because these are four REST calls and one of them
cannot be undone for a fortnight:
[`scripts/daily-phone-number.sh`](../../scripts/daily-phone-number.sh).

See what is available, then buy one:

```sh
scripts/daily-phone-number.sh available US
scripts/daily-phone-number.sh buy +1XXXXXXXXXX
```

It asks you to type `yes` first, because the call spends money. Omit the number
and Daily picks one for you.

You get back an `id` and a `number`. **Keep the id.** Outbound calling wants the
id as its `callerId`, not the number.

```json
{ "id": "85a70a9a-e22f-4a8d-8302-bbf1b88dd909", "number": "+18058700061" }
```

`scripts/daily-phone-number.sh list` prints them again if you lose it.

> **A number cannot be released for 14 days after purchase**, and releasing is
> then permanent. That is Daily's rule, not ours. Plan the test rig knowing the
> number is yours for at least two weeks.

The raw calls, if you would rather not use the helper:

```sh
curl --request GET --url 'https://api.daily.co/v1/list-available-numbers?region=US' \
  --header "Authorization: Bearer $DAILY_API_KEY"

curl --request POST --url 'https://api.daily.co/v1/buy-phone-number' \
  --header "Authorization: Bearer $DAILY_API_KEY" \
  --header 'Content-Type: application/json' \
  --data '{"number": "+1XXXXXXXXXX"}'
```

### 3. Set the destination and check the package

The transfer destination is the **name** of an env var holding an E.164 number or
a `sip:` URI, read at call time. A committed example never ships a dialable
literal: any number a repository ships is a number nobody answers.

```sh
# .env, which is gitignored
BILLING_PHONE_NUMBER=+1XXXXXXXXXX
```

```sh
bin/unmute validate examples/pipecat-human-transfer-daily
```

Expect `✓ pipecat`, and on stderr the dial-out prerequisite from step 1, with the
page it came from. **Exit code 0.** The package is correct; whether your account
is provisioned is not something a compiler can know.

### 4. Compile

```sh
bin/unmute compile examples/pipecat-human-transfer-daily
```

That writes `build/pipecat/`. Look at what it did **not** emit: no Redis, no
`/telephony/inbound`, no media websocket. On this route Daily's infrastructure
carries the call, so the project listens on no public port and you host no
webhook.

`build/` is disposable and gitignored. Recompiling is always safe.

### 5. Deploy to Pipecat Cloud

The emitted `build/pipecat/README.md` prints the exact commands. **The order
matters:**

```sh
cd examples/pipecat-human-transfer-daily/build/pipecat
cp .env.example .env          # then fill in the values, including BILLING_PHONE_NUMBER
pipecat cloud secrets set <set-name> --file .env
pipecat cloud deploy
pipecat cloud agent status <agent-name>
```

Create the secret set **first**: the emitted `pcc-deploy.toml` already names it,
and a deploy cannot start without it. `deploy` builds the image in the cloud from
the emitted `Dockerfile`, which is why the manifest names no image.

Wait for `status` to report **`ready`**. That, not a successful deploy command, is
the deploy being usable.

If the set name is taken (they are globally unique across the whole platform),
create yours under another name and pass `--secrets <other-name>` to `deploy`.

Declaring a region? Then the secret set needs the same `--region`. A set in the
wrong region cannot be read by the agent, and the failure names neither side.

### 6. Attach the number to the deployed agent

Now, and not before: the webhook names the agent, so a number pointed at an agent
that is not deployed gives the caller **silence**, not an error you can see.

Easiest path is the dashboard: **Pipecat Cloud → Settings → Telephony**, pick the
number, pick this agent, save.

Or with the helper:

```sh
scripts/daily-phone-number.sh attach +1XXXXXXXXXX <org-id> <agent-name>
```

Or raw. The webhook is the platform's own; you run nothing:

```sh
curl --location 'https://api.daily.co/v1' \
  --header "Authorization: Bearer $DAILY_API_KEY" \
  --header 'Content-Type: application/json' \
  --data '{"properties": {"pinless_dialin": [
      {"phone_number": "+1XXXXXXXXXX",
       "room_creation_api": "https://api.pipecat.daily.co/v1/public/webhooks/<org-id>/<agent-name>/dialin"}
  ]}}'
```

`<org-id>` is in the Pipecat Cloud dashboard. Note this call **replaces** the
domain's whole `pinless_dialin` list: if you have other numbers attached, send
them all together or use the dashboard.

### 7. Test it

Four calls. Do them in order; each one can only fail in ways the previous has
ruled out.

**7a. The answer test.** Call the number. The agent should pick up and hold a
conversation.

*This is the step that failed before 2026-08-12.* The generated bot handed
Pipecat's runner a parameter object that rejects an inbound call's own details, so
every call died while the transport was being built. If it fails now, look at the
agent logs before suspecting your account.

**7b. The cold transfer.** Ask about an invoice, a refund, or a charge you do not
recognise. Expect, in this order:

1. one spoken handoff line, within about two seconds
2. the phone at `BILLING_PHONE_NUMBER` rings
3. the agent leaves the call

**7c. The failure drill.** Point `BILLING_PHONE_NUMBER` at an undialable number,
redeploy the secret set, call again, ask for the transfer.

Expect the caller to stay connected and be told it did not work, with the agent
carrying on. On this transport a failed transfer is a **return value, not an
exception**, and the generated tool reads it and applies `on_unavailable`. This
package uses the default, `return_to_caller`; `hangup` would instead say a goodbye
line and end the call.

**7d. The double-request drill.** Call again and ask to be put through twice in
quick succession.

Expect **exactly one** transfer attempt. The bot keeps the first attempt's answer
and replays it, so a second ask cannot fire a second REFER, and a transfer that
failed cannot come back as a success. Before 2026-08-12 there was no guard here at
all and a second ask dialled again.

### 8. Record the result

The run is not finished until [docs/TRANSFERS.md](../../docs/TRANSFERS.md) carries
the dated outcome. Its Status table is per row, and the Pipecat Daily row is
provisional until someone runs this as written.

If a step above was wrong, **fix this file and that document before touching
code**. That is the rule those documents set for themselves.

### 9. Tear the rig down

A test rig must not become a standing bill.

```sh
pipecat cloud agent delete <agent-name>
```

Then remove the secret set if you made it for the test, and release the number
**if it is at least 14 days old**:

```sh
scripts/daily-phone-number.sh list                  # find the id
scripts/daily-phone-number.sh release <id>
```

If it is younger than 14 days, Daily will refuse. You keep paying for it until
then; there is no way around that, so note the purchase date somewhere.

---

## Local development

You can talk to this agent right now, with no accounts and no phone:

```sh
bin/unmute dev examples/pipecat-human-transfer-daily              # browser
bin/unmute dev --console examples/pipecat-human-transfer-daily    # terminal
```

`--telephony` **refuses on this route**, by name, and tells you the same thing:
Daily carries phone calls to a deployed agent, so there is no local telephony
topology to run. Asking for a transfer in a browser session gets you a clean
"this session is not a phone call" rather than a crash.
