# Unmute CLI

Unmute is SLNG's portability layer for voice agents: author the agent once as
a directory of declarative YAML, then compile or apply it to the orchestration
layer you choose. `unmute init` scaffolds an SLNG-bound project (one
`SLNG_API_KEY` covers hosted STT and TTS) unless you change a model; any
provider a framework integrates is a one-line model change
([provider reference](docs/user/reference/providers.md)).

Commands: `init`, `validate`, `compile`, `apply`, `dev`
([CLI reference](docs/user/reference/cli.md)). Drivers shipped today:
**Pipecat** and
**LiveKit** (code targets, `compile` writes a runnable Python project) and
**ElevenLabs** (managed target, `apply` reconciles the provider config). Vapi
and Deepgram fail with `driver is not implemented` until theirs land.

Where things live: user docs in [docs/user/](docs/user/README.md), the locked
schema in [SCHEMA.md](SCHEMA.md), driver specs in [docs/spec/](docs/spec/),
the provider catalogue design and findings in
[PROVIDER_CATALOG.md](PROVIDER_CATALOG.md), vocabulary in
[CONTEXT.md](CONTEXT.md), engineering rules in [CLAUDE.md](CLAUDE.md).

## Build

```sh
make build        # writes bin/unmute
bin/unmute --help
make install      # into your Go bin path
```

Direct equivalent: `CGO_ENABLED=0 go build -o bin/unmute .`

## Docs

The user guides in [docs/user/](docs/user/README.md) render as a searchable
site with no build step — [docsify](https://docsify.js.org) serves the Markdown
in place from a CDN (needs `npx` on your PATH).

```sh
make docs          # serves docs/user/ on http://localhost:3000
```

Then open `http://localhost:3000` and browse the Start / Learn / Concepts /
Reference / Targets sidebar. Edit any `docs/user/**/*.md` and the open page
live-reloads on save. Only `docs/user/` is served, so the engineering specs in
`docs/spec/` stay out of the site.

Direct equivalent: `npx --yes docsify-cli serve docs/user --port 3000`.

## Test

The default gate is pure Go and needs zero Python:

```sh
make test         # = go test ./...  (L1 unit, L2 command, L3 golden)
make lint
make fmt          # gofmt -w . && go vet ./...
```

Regenerate goldens after an intentional output change:

```sh
go test ./internal/scaffold -update
go test ./internal/generate -run TestPipecatV1 -update-pipecat
go test ./internal/generate -run TestLiveKitV1RemyGolden -update-livekit
go test ./internal/generate -run TestCatalogResolutionGolden -update-catalog
```

L4 smoke is opt-in (needs `uv` and network; skips without `uv`):

```sh
make smoke
```

Smoke resolves the emitted `pyproject.toml` into a real venv, imports the
generated `bot.py`, and instantiates every emitted service constructor against
the installed packages, so provider kwarg drift fails here rather than at a
user's first run.

## Try the CLI end to end

Every [example](examples/README.md) declares both `pipecat` and `livekit`, so
`validate` and `compile` cover both targets. Runs need `uv` on your PATH. Keep
credentials in the ignored repo-root `.env.local` (the generated
`.env.example` files list the exact variables), and copy them after compiling,
because recompiling replaces `build/<target>/`.

```sh
make build

# Choose: simple-prompt, single-task, task-groups, or subagents
EXAMPLE=simple-prompt

bin/unmute validate "examples/$EXAMPLE"
bin/unmute compile "examples/$EXAMPLE"
```

Test the generated LiveKit project in console mode (terminal mic and speaker):

```sh
cp .env.local "examples/$EXAMPLE/build/livekit/.env.local"
cd "examples/$EXAMPLE/build/livekit"
uv run agent.py console
```

Test the generated Pipecat project (starts a WebRTC test client and prints the
URL to open). Run from the repo root:

```sh
cp .env.local "examples/$EXAMPLE/build/pipecat/.env"
cd "examples/$EXAMPLE/build/pipecat"
uv run bot.py
```

`unmute dev` does the recompile-and-run loop in one command, reading `.env`
from the package root:

```sh
cp .env.local "examples/$EXAMPLE/.env"
bin/unmute dev "examples/$EXAMPLE" --target pipecat
bin/unmute dev "examples/$EXAMPLE" --target livekit --console
```

## Telephony

Telephony compilation targets LiveKit and Pipecat. A target binds one
`connections/<name>.yaml` file to an exact transport and carrier route. The
Connection stores environment variable names only; credential values stay in
an ignored `.env` file or your deployment secret store.

A package may declare any number of supported carrier routes. Give each one a
named target, such as `pipecat_twilio`, `pipecat_telnyx`, or `livekit_plivo`,
and bind each target to one Connection. Every target produces a separate,
single-carrier project in `build/<target-name>/`; Unmute never combines carrier
SDKs or route-specific limits inside one generated runtime.

```yaml
# connections/primary_phone.yaml
kind: telephony
environment:
  account_sid: TWILIO_ACCOUNT_SID
  auth_token: TWILIO_AUTH_TOKEN
  from_number: TWILIO_PHONE_NUMBER
```

Route support remains provisional until a real credentialed smoke passes.
[TELEPHONY.md](TELEPHONY.md) documents the architecture, exact credential list,
where to obtain each credential, local public-URL flow, deployment topology,
and current verification policy.

Every generated telephony project includes `compose.telephony.yaml`, and
`unmute dev <agent> --target <name> --telephony` always executes it. Pipecat
runs the generated application plus Redis. LiveKit SIP runs the generated
Agent, Redis, LiveKit Server, and LiveKit SIP. Compose supplies local Redis and
the conspicuous non-production LiveKit key pair; put carrier and model keys in
the package's ignored `.env`. A normal `ctrl-c` stops only that package's
Compose project and preserves its Redis volume.

Generated Pipecat adapters currently cover Twilio, Telnyx, and Plivo offline;
all stay provisional until credentialed smokes pass. For Twilio, get the
Account SID and Auth Token from the Console account dashboard and the caller ID
from Phone Numbers. For Telnyx, get an API key and webhook public key from
Mission Control, then use a Voice API Application ID as
`TELNYX_CONNECTION_ID`. For Plivo, get the Auth ID and Auth Token from the
Console dashboard and the caller ID from Phone Numbers. Every Pipecat carrier
WebSocket route needs `UNMUTE_PUBLIC_URL` set to the exact public HTTPS origin;
generate a separate `UNMUTE_OUTBOUND_TOKEN` if the channel permits outbound
calls. See the linked telephony guide for the exact Connection vocabulary and
setup steps.

Generated LiveKit SIP projects cover Twilio, Telnyx, and Plivo through the
same `livekit/sip/<carrier>` plan. Their Connection vocabulary is:

```yaml
# connections/primary_phone.yaml
kind: telephony
environment:
  sip_address: TWILIO_SIP_ADDRESS
  sip_username: TWILIO_SIP_USERNAME
  sip_password: TWILIO_SIP_PASSWORD
  from_number: TWILIO_PHONE_NUMBER
```

Set the target's `provider: livekit`, `transport: sip`, `carrier`, and
`connection`. The generated project includes inbound-trunk, outbound-trunk,
and dispatch-rule JSON inputs for the directions you request. Self-hosted SIP
also requires `LIVEKIT_URL`, `LIVEKIT_API_KEY`, `LIVEKIT_API_SECRET`,
`REDIS_URL`, and a public `LIVEKIT_SIP_URI` in deployment; local Compose
supplies the Server, key pair, and Redis connection. The `lk` commands return
the trunk IDs used at runtime. An HTTPS tunnel is enough for Pipecat callbacks,
but not for LiveKit SIP signaling and RTP.

Exotel remains gated: its documented static Voicebot WebSocket flow does not
yet provide a proven authenticated upgrade that satisfies Unmute's ingress
policy, and its LiveKit SIP route has no provider-specific proof. LiveKit's
Beta Twilio Connector route is also gated: its route and credential vocabulary
are recognized, but Unmute does not emit a Connector adapter or inherit SIP
transfer capabilities.
