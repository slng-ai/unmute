# Twilio walkthrough, step by step

This guide walks you through testing real phone calls with the
[twilio-telephony-hello example](../../../examples/twilio-telephony-hello/README.md),
inbound and outbound, on the route each platform recommends for Twilio:

- **Pipecat** on `transport: cloud-websocket`: Twilio streams the call's audio to
  Pipecat Cloud, named by a static TwiML Bin. You host nothing.
- **LiveKit** on `transport: sip`: a Twilio Elastic SIP Trunk carries the call into
  LiveKit SIP. LiveKit Cloud can run the worker for you.

These are two different mechanisms, not two spellings of one thing, and the
difference decides what you can test where. **Twilio reaches Media Streams over
HTTPS and WSS**, which a tunnel carries, so an inbound call can land on your laptop.
**SIP is signalling plus RTP media**, which a tunnel cannot carry, so an inbound SIP
call needs a routable host. Outbound is the mirror image: a Pipecat outbound call is
created at Twilio naming the *deployed* agent, while a SIP outbound call starts from
your side and therefore works locally.

So between the two targets you can hear both directions from a laptop, just not both
on the same target:

| | Pipecat `cloud-websocket` | LiveKit `sip` |
|---|---|---|
| local inbound | **yes** (Part 1) | no, deploy first |
| local outbound | no, deploy first | **yes** (Part 2) |

If you want the concepts behind these steps, read
[07. Phone calls](07-phone-calls.md) first.

> Both routes have real adapters, so validation, compilation, and
> `unmute dev --telephony` run them cleanly, with no warning. The commands below
> work today. Test the behaviour you depend on yourself before you rely on it in
> production.

## What you need

- A Twilio account with some balance (calls and numbers cost money).
- A real phone to call from and to.
- The Unmute CLI built (`make build`).
- Model provider keys (`OPENAI_API_KEY`, `SLNG_API_KEY`) so the agent can think,
  hear, and speak.
- For Part 1: `uv` on PATH, and `cloudflared` on PATH (or your own tunnel).
- For Part 2: Docker with the Compose plugin, and a Twilio Elastic SIP Trunk.

Create the environment file the example reads:

```bash
cat > examples/twilio-telephony-hello/.env <<'EOF'
# both parts
OPENAI_API_KEY=
SLNG_API_KEY=

# Part 1, the Pipecat route
TWILIO_ACCOUNT_SID=
TWILIO_AUTH_TOKEN=
TWILIO_PHONE_NUMBER=
PIPECAT_CLOUD_ORGANIZATION=

# Part 2, the LiveKit route
SIP_TRUNK_HOSTNAME=
SIP_AUTH_USERNAME=
SIP_AUTH_PASSWORD=
SIP_FROM_NUMBER=
EOF
```

Everything below fills that in. The dev command supplies `UNMUTE_PUBLIC_URL`,
`LIVEKIT_URL`, `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET`, and `REDIS_URL` by itself.
Leave those out.

**One number serves one target at a time.** A number attached to a SIP trunk ignores
its voice configuration, so the same number cannot also point at a TwiML Bin. Take
it off the trunk for Part 1, and put it back for Part 2.

## Part 1: the Pipecat route (Media Streams to Pipecat Cloud)

### Step 1: get the account credentials

1. Sign in at [console.twilio.com](https://console.twilio.com).
2. On the Console home page, find the **Account Info** card.
3. Copy **Account SID** (starts with `AC`) into `TWILIO_ACCOUNT_SID`.
4. Click **Show** next to **Auth Token** and copy it into `TWILIO_AUTH_TOKEN`.

The Auth Token verifies webhook signatures on the local run and authorizes the REST
calls that place an outbound call and redirect a transfer.

### Step 2: get a Voice-capable number

1. In the Console, go to **Phone Numbers → Manage → Buy a number**.
2. Filter by **Voice** capability and buy one, or pick an existing number under
   **Phone Numbers → Manage → Active numbers**.
3. Copy the number in E.164 form (for example `+15550001111`) into
   `TWILIO_PHONE_NUMBER`.

You do not configure the number's webhook by hand for a local session. The dev
command sets it on every start, prints the previous value, and puts that value back
when you stop, interrupt included. Pass `--no-webhook` if it should leave your
number alone.

### Step 3: get your organization slug

```bash
pipecat cloud organizations list
```

Copy the hyphenated machine slug (something like `zonal-bison-orange-168`) into
`PIPECAT_CLOUD_ORGANIZATION`. It is **not** your display name, and the CLI's heading
for that column has changed between versions, so read the value's shape rather than
the heading. The compiler cannot know it, and the markup a call arrives on has to
name your agent as `<agent>.<organization>`.

### Step 4: install cloudflared

Twilio must reach your machine over public HTTPS for the local run. The dev command
manages a
[cloudflared quick tunnel](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/do-more-with-tunnels/trycloudflare/)
for you; it only needs the binary on PATH:

```bash
brew install cloudflared
```

On Linux, install the distribution package or download a binary from the
cloudflare/cloudflared releases page. No Cloudflare account is needed. If you prefer
ngrok or another tunnel, skip this step and pass
`--public-url https://your-tunnel.example` instead.

### Step 5: run it and test inbound

```bash
unmute dev examples/twilio-telephony-hello --target pipecat --telephony
```

Read the output top to bottom:

1. The resolved route and setup notes.
2. `managed tunnel https://<random>.trycloudflare.com` (rotates per run).
3. `Twilio voice webhook for +1... set to ... (was: ...)`. Note the `was` value if
   you want to restore it yourself.
4. A line confirming your deployed TwiML Bin was **not** touched: the local runner
   answers Twilio's webhook itself, so nothing is created at your carrier.
5. `call +1XXXXXXXXXX, ctrl-c to stop`.

Call that number. The agent answers and greets you; speak and it replies. No Docker
is involved: this route runs the agent with `uv` and nothing else.

### Step 6: outbound is a deploy exercise here

`--to` on this target prints why it does nothing rather than pretending: an outbound
call's markup names the **deployed** agent, so the call goes to Pipecat Cloud and
there is nothing local for it to reach. Deploy first:

```bash
unmute compile examples/twilio-telephony-hello
cd examples/twilio-telephony-hello/build/pipecat
cp .env.example .env                        # then fill it in
pipecat cloud secrets set <set-name> --file .env --region eu-central
pipecat cloud deploy
pipecat cloud agent status <agent-name>     # wait for ready
```

The generated `build/pipecat/README.md` then gives you two things this page does
not copy: the exact TwiML Bin markup with your values in it, and the one command
that places an outbound call. Its **Telephony setup** section is the authority.

## Part 2: the LiveKit route (Twilio Elastic SIP Trunk)

The trunk side is Twilio Elastic SIP Trunking by decision: LiveKit Phone Numbers are
inbound-only and cannot transfer. Compile the package first, because
`build/livekit/README.md` dictates each console step with your own variable names,
the console paths, and a runnable command for each:

```bash
unmute compile examples/twilio-telephony-hello
```

### Step 1: create the trunk and read its three dial-out values

In **Elastic SIP Trunking → Manage → Trunks**, create a trunk, then:

1. **Termination** gives you the trunk's own domain, ending in `pstn.twilio.com`,
   plus a Credential List username and password. That domain is one value, not two.
   They go into `SIP_TRUNK_HOSTNAME`, `SIP_AUTH_USERNAME`, and `SIP_AUTH_PASSWORD`.
2. **Origination** points at your LiveKit project SIP URI with `;transport=tcp`.
   That URI comes from the **project ID**, not from `LIVEKIT_URL`; the generated
   README prints yours with one `lk` command.
3. Put your number in E.164 into `SIP_FROM_NUMBER`. The same number as Part 1 is
   fine.

### Step 2: attach your number to the trunk

This is the step people miss. A number that is not on the trunk never reaches
LiveKit, however correct the origination URI is. The generated README has the CLI
commands and the check that prints the trunk SID rather than `none`.

Doing this takes the number off Part 1's voice configuration, which is expected.

### Step 3: place an outbound call locally

```bash
unmute dev examples/twilio-telephony-hello --target livekit --telephony --to +15551234567
```

The Compose graph here is the agent, Redis, LiveKit Server, and LiveKit SIP, and the
dev command creates the local trunk and dispatch records itself. Once the graph is
healthy it dispatches the agent and the call goes out through your trunk, dialling
with the four `SIP_*` values passed inline, so **no LiveKit outbound trunk is
registered**. Your phone rings and the agent talks when you answer.

If the call connects but you hear nothing, that is the RTP path back to the
container rather than anything in the agent. The local SIP section of
[TELEPHONY.md](../../TELEPHONY.md) covers it.

### Step 4: inbound is a deploy exercise here

An inbound SIP call needs the carrier to reach SIP signalling and an RTP range at a
routable address. An HTTPS tunnel is neither required nor sufficient, and every
container in the local graph can be healthy while the call still never arrives. So
deploy, then call:

```bash
cd examples/twilio-telephony-hello/build/livekit
cp .env.example .env                                      # then fill it in
lk agent create --region eu-central --secrets-file .env   # first deploy only
bash telephony-setup.sh                                   # inbound trunk + dispatch rule
lk agent status
```

`lk agent create` is the first deploy and writes the `livekit.toml` the build
directory deliberately does not ship, because the platform assigns both of its
values. Every later version is `lk agent deploy`. `telephony-setup.sh` resolves the
inbound trunk by the phone number you already have, so no record ID is copied by
hand; run it twice and the second run should report everything reused.

Then call your number.

## Part 3: transfer the caller to a person

Only one of these two routes can hold a caller while it briefs a person, and the
reason is the mechanism rather than the effort:

- **LiveKit `sip`** has both shapes on LiveKit's own primitives: cold is a SIP REFER
  through the trunk, warm is `WarmTransferTask`. Cold needs **Call Transfer (SIP
  REFER)** enabled on the trunk with **Enable PSTN Transfer** ticked, or the carrier
  rejects it.
- **Pipecat `cloud-websocket`** has cold only, by replacing the live call's markup at
  Twilio. Warm cannot work there: once the markup is replaced the agent has no leg to
  hold and nothing tells it how the other side's leg ended.

The map, the yaml, the secrets, and the cloud test walkthroughs are in
[TRANSFERS.md](../../TRANSFERS.md).
[`examples/livekit-human-transfer`](https://github.com/slng-ai/unmute_cli/tree/main/examples/livekit-human-transfer)
does both shapes on LiveKit SIP, and
[`examples/pipecat-human-transfer-twilio`](https://github.com/slng-ai/unmute_cli/tree/main/examples/pipecat-human-transfer-twilio)
does cold on Pipecat Cloud.

One useful preview needs no phone number at all: deploy the LiveKit example and open
the LiveKit Agent Console, ask for a manager, and the warm transfer rings the
supervisor's real phone while you talk from the browser. The cold transfer cannot be
previewed that way, because it refers the caller's *existing* SIP leg and a browser
session has none.

## The third option: Media Streams you host yourself

Two more routes carry Twilio Media Streams to a socket of **yours**: Pipecat
`carrier-websocket` and LiveKit `connector`. Both are laptop-testable in **both**
directions, which the two routes above are not, and
[`examples/outbound-reminder`](https://github.com/slng-ai/unmute_cli/tree/main/examples/outbound-reminder)
declares them. They are also where you go when the agent needs a variable sourced
from the caller's number, because the code that fills those variables is the carrier
adapter only these routes emit.

The trade, being precise about why you would move off them:

| | Media Streams you host | Pipecat `cloud-websocket` | LiveKit `sip` |
|---|---|---|---|
| Telephony layer | ours, so each capability is our work | Pipecat Cloud's | LiveKit's, maintained upstream |
| You host | a webhook and a media socket, forever | nothing | the worker |
| Media transport | TCP: a lost packet stalls the ones behind it | TCP, same | RTP over UDP: a lost packet is a click |
| Cost per minute | Programmable Voice, roughly double SIP | Programmable Voice | SIP trunking |
| Human transfer | none: media only | cold only, session does not survive it | both shapes, native |
| Carriers | Twilio, Telnyx, Plivo | Twilio | any SIP trunk provider or PBX |
| Caller-number variables | yes | refused by name | yes |

Move to SIP for transfers, scale, multi-carrier, or LiveKit-maintained machinery.
Move to `cloud-websocket` to host nothing at all. Stay on a self-hosted Media
Streams route when you need the caller's own call details bound to variables. See
[07. Phone calls](07-phone-calls.md) and
[Configure LiveKit in YAML](../targets/livekit.md) for the fields each one needs.

## Troubleshooting

- **`cloudflared not found on PATH`**: install it (Part 1, Step 4) or pass
  `--public-url` with your own tunnel.
- **`missing telephony credentials/configuration: ...`**: the named `.env` values are
  empty. The error lists exactly which ones.
- **The call connects and the agent never speaks (Part 1)**: check the organization
  value in your TwiML Bin. A display name where the slug belongs is refused by the
  platform before the agent starts, so the agent's own log stays empty.
- **`phone number ... was not found on this Twilio account`**: the number in
  `TWILIO_PHONE_NUMBER` does not exist on this account or is not E.164. Check
  **Phone Numbers → Manage → Active numbers**.
- **An inbound call rings and drops with no agent (Part 2)**: work backwards through
  the number being attached to the trunk, the records `telephony-setup.sh` creates,
  and the agent being up. In that order; each one can only fail in ways the previous
  has ruled out.
- **Outbound call rings then drops, or never rings**: on a trial account Twilio only
  calls verified numbers, and international destinations need
  **Voice → Settings → Geo Permissions** enabled for that country.
- **A cold transfer is rejected by the carrier (Part 2)**: the trunk's transfer mode.
  Expect `enable-all`; `disable-all` or `sip-only` fails every cold transfer to a
  phone number, and no change on the LiveKit side compensates.

## Restore Twilio when you are done

- Set the number's voice webhook back to the `was:` value the command printed, under
  **Phone Numbers → Manage → Active numbers → your number → Voice Configuration**.
- Detach the number from the trunk if you want its voice configuration back.
- Release test numbers you no longer need; they bill monthly.
- Tear down what you deployed, so a test rig does not become a standing bill:
  `pipecat cloud agent delete <agent-name>`, the secret set if you made it for the
  test, and the LiveKit inbound trunk and dispatch rule
  (`lk sip inbound list` / `lk sip dispatch list` to find them). The full checklist
  is the Teardown section of [TRANSFERS.md](../../TRANSFERS.md).

Next: [08. Going live](08-going-live.md) for production ingress, secrets, and
capacity.
