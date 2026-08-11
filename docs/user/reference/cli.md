# Reference: the CLI

`unmute` has four commands: `init`, `validate`, `compile`, and `dev`.
All except `init` read a v1 package; see [agent.yaml](agent-yaml.md). Two
drivers are shipped: **Pipecat** and **LiveKit**, both code targets.
Validation works for all four providers. Generation and development commands
report `driver is not implemented` for Vapi and Deepgram until their drivers
land.

**Exit codes:** `0` on success, `1` on error. Warnings go to standard error and still exit `0`; they never silently downgrade a result.

## init

```sh
unmute init [name]
```

Scaffolds a new v1 package. The noninteractive default is exactly `agent.yaml`, `instructions.md`, `targets.yaml`, and `.env.example`, with a ready-to-run `pipecat` target. Extra agents, tasks, and tools add their prompt or manifest files. Prints a `created <path>` line for each file.

- With a `name`, writes the package to that directory.
- With no argument on an interactive terminal, opens the creation wizard. Start
  with target, language, STT/LLM/TTS models, prompt, and greeting; optionally
  add variables, tools, agents, directional handoffs, typed tasks, ordered task
  groups, web/phone channels, and human transfers. Saved items remain visible
  in their section and open into one edit screen with a delete action; deleting
  also removes or resets dependent references. The required starter agent and
  default models offer **Reset** instead. Every reference picker lists the
  compatible items already created. Advanced conversation, fallback, capacity,
  and target settings stay under **Customize**.
- Provider choices come from the selected target's catalogue and each provider brand appears once. When a brand is available through more than one distributor, such as Cartesia directly or through SLNG, the wizard asks for the distributor next. Model ids, voice ids, and provider params remain forwarded text; the wizard does not invent an allowlist for them.
- A failed preflight opens a dedicated repair menu instead of printing above the creation menu. From there you can open the relevant saved-resource screens to edit or delete the failing configuration, then retry creation.
- Tools are execution-neutral in the menu. Webhook is available in shipped starters; Local Python remains visible with the selected driver's exact support limitation when that driver cannot emit a handler.
- Task results start as `{"result":"string"}`. Each key is one returned field whose value is a primitive type (`string`, `number`, `boolean`, or `integer`) or an enum; result-to-variable assignment uses pickers rather than handwritten JSON.
- Handoffs stay unavailable until two agents exist. Task groups stay unavailable until a task exists. Telephony declares required behavior and target transport/carrier only—it does not provision a phone number, SIP trunk, room, or carrier account.
- Before the review screen, the candidate is rendered in a temporary directory
  and sent through the real load, build, and selected-target generator. The
  review shows target warnings, required environment names, and forwarded
  models. A failing candidate is not written; its exact cause appears on a
  dedicated **Cannot create agent** screen with **Back**. Nothing reaches the
  destination until final confirmation.
- **Back** preserves earlier edits. In accessible mode, enter `:back`; in the keyboard UI, press Esc.
- It refuses to write into a directory that already exists and is non-empty, rather than overwrite an agent.

## validate

```sh
unmute validate <package-dir> [--target <name>]...
```

Loads, builds, and checks the package against its targets, without generating
anything. Prints one status line per target:

```text
✓ livekit (livekit)
✓ pipecat (pipecat)
```

The check mark becomes `✗` for a failing target. Warnings and errors print in
separate, labeled blocks on standard error, with each message naming its target.
The command exits `1` if any target fails.

`validate` uses the schema's capability rules and the provider catalogue, not a driver, so it works for **all four providers** whether or not their driver is shipped. Use it to check portability across targets before you commit to one. Repeat `--target` to check specific instances, or omit it to check every declared target.

## compile

```sh
unmute compile <package-dir> [--target <name>]...
```

Runs validate, then generates. For a **code target** (Pipecat, LiveKit), writes the project to `<package-dir>/build/<target-name>/` and prints a `generated <path>` line per file. Omitting `--target` compiles every declared target.

Compiling a Vapi or Deepgram instance fails with `<provider> driver is not implemented` until those drivers ship.

## dev

```sh
unmute dev <agent-dir> [--target <name>] [--var name=value ...] [--console | --telephony [--public-url <https-url>] [--to <e164>]] [--port 8765] [--bot-port 7860] [--no-open] [--verbose]
```

The fastest loop for a **Pipecat or LiveKit** instance: compiles the selected target to `build/<name>/`, runs it locally, and lets you talk to the agent — in the browser (default) or in your terminal (`--console`). Whatever you build, you can speak to.

- With exactly one declared target, `dev` selects it automatically.
- With multiple targets on an interactive terminal, `dev` always asks which instance to test and shows both instance name and provider. In a noninteractive shell, pass `--target <name>`; it never picks by YAML/map order or by preferring Pipecat.
- `--target` dispatches that exact instance. Pipecat and LiveKit both run; unshipped providers (Vapi, Deepgram) report their own missing dev runner.

**Web (default), runs the deployable container.** This is the default mode
and it **requires Docker**. `unmute dev` builds the same image production
deploys and talks to it through one standardized SLNG-branded web UI over
WebRTC, the same page for both Pipecat and LiveKit. It generates the project,
emits a `compose.dev.yaml` next to the `Dockerfile`, runs
`docker compose up --build --wait` under a project-scoped name, serves the UI,
and tears the stack down on exit (data volumes are kept). The only per-target
difference is the transport adapter behind `GET /api/session`:
- Pipecat: one `application` container runs `python bot.py --host 0.0.0.0 --port 7860`; the page POSTs its WebRTC offer, which the dev server proxies to the container.
- LiveKit: a single-node `livekit-server --dev` container plus the `application` worker running `python agent.py dev`; the page mints a token and joins the room, and the worker joins it automatically. No LiveKit install, no host server, no credentials: the containerized dev server uses the `--dev` key pair, and the browser reaches it on the published ports.

The old host-`uv` web path is gone: local testing always runs the deployable
image, so `uv` is **not** needed for web mode. If Docker or the Compose plugin
is missing, `dev` fails before doing any work with an install message naming
Docker Desktop or Docker Engine plus the Compose plugin, and points you at
`--console` to talk without Docker.

**Console (`--console`).** Talk in the terminal over your local mic and speaker. No browser, no Docker, no dev server:
- Pipecat: `uv run --extra console bot.py console`. The `console` extra pulls in pyaudio; on macOS run `brew install portaudio` first.
- LiveKit: `uv run agent.py console`. Needs **no** LiveKit credentials for a
  scaffold-default agent with native providers and local turn detection. It
  only needs `LIVEKIT_API_KEY` and `LIVEKIT_API_SECRET` if a model routes
  through LiveKit Inference, such as a think model with `provider: livekit` or
  the cloud `turn-detector`. The preflight tells you which.

**Telephony (`--telephony`).** Real phone calls, through the generated
`compose.telephony.yaml`. A route with a real adapter (every Pipecat carrier
WebSocket route, the LiveKit Twilio connector, and every LiveKit SIP route) just
runs: validation, compilation, and `dev --telephony` start it with no warning
and no verification error. Only a **gated** route with no adapter at all
(Exotel) still fails closed, because there is nothing to run. Test the behavior
you depend on yourself before production.

The LiveKit Twilio connector runs like the Pipecat carrier route. It uses the
same three Twilio credentials (`TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`,
`TWILIO_PHONE_NUMBER`), no SIP trunk and no Redis. Its generated
`telephony_bridge.py` speaks the Twilio Media Streams protocol and bridges the
call into a local, self-hosted LiveKit room, so it needs no LiveKit Cloud.
`dev --telephony` starts a managed cloudflared tunnel, sets the Twilio voice
webhook automatically, and places an outbound call with `--to`. Twilio only
reaches the bridge over HTTPS and WSS, so both inbound and outbound work fully
on a laptop. It supports inbound, outbound, hangup and both transfer shapes;
voicemail detection stays on the LiveKit SIP route.

Telephony mode runs the generated `compose.telephony.yaml`; there is no
host-process fallback or infrastructure flag. Pipecat carrier and LiveKit SIP
routes build the generated application and version-pinned Redis; the LiveKit
Twilio connector builds the application plus a local `livekit-server --dev` and
needs no Redis. LiveKit SIP additionally starts
version-pinned LiveKit Server and LiveKit SIP, then creates or reuses the local
inbound trunk, outbound trunk, and dispatch rule itself and injects
`LIVEKIT_SIP_INBOUND_TRUNK` and `LIVEKIT_SIP_OUTBOUND_TRUNK` before the
application starts. Unmute preflights Docker Compose, waits for declared health
checks, prints the resolved service graph and carrier setup, configures the
Twilio voice webhook automatically where the route carries that fact (printing
the previous value), prints the call line, places an outbound call when `--to`
is set, follows Compose logs under `--verbose`, and stops only its deterministic
project on `ctrl-c` without removing data volumes or leaving the tunnel
running.

**Outbound dial-out (`--to <e164>`).** On an outbound-capable target, pass
`--to +15551234567` and, once the container is healthy, the dev command places
one call to that number and prints the returned call id. The CLI mints the
dial-out secret `UNMUTE_OUTBOUND_TOKEN` itself, injects it into the container,
and never demands it from `.env` or prints it. `--to` requires `--telephony`
and a resolved direction that includes outbound; it is rejected on an
inbound-only target. Without `--to`, an outbound-capable target prints how to
place a call and dials nothing. The generated `POST /telephony/outbound`
endpoint stays available for your own application to drive.

Routes with public HTTP/WSS endpoints (Pipecat carrier WebSockets and the
LiveKit Twilio connector) need a public HTTPS origin. Without `--public-url`,
the dev command starts a managed
cloudflared quick tunnel and supplies `UNMUTE_PUBLIC_URL` itself; cloudflared
must be on PATH (macOS: `brew install cloudflared`; Linux: distribution
package or the cloudflare/cloudflared releases page). `--public-url` skips
all tunnel management and must be the exact public HTTPS origin used for
carrier signatures (use it for ngrok or any other tunnel). LiveKit SIP has no
HTTP callback URL; it instead needs carrier-reachable SIP and RTP networking,
which no HTTPS tunnel provides. Unmute names missing carrier/model
configuration after generation and points to the credential guide without
printing values. Local Compose supplies Redis for both targets and the local
LiveKit Server key pair. Explicit external LiveKit or Redis values, and user-set
trunk IDs, are rejected in LiveKit SIP dev mode rather than silently ignored.

When a package declares several carrier routes, each one is a separate target
and a separate generated Compose project. The package and
schema can hold any number of supported routes. Today each telephony target
fails closed; after promotion, `compile` can select several targets or all of
them, while `dev --telephony` runs one exact route at a time. Pass its instance
name, such as `--target pipecat_twilio` or `--target livekit_plivo`.

**Seeding variables (`--var name=value`).** Repeat the flag once per variable.
It is the local stand-in for the dispatch payload production sends, so it works
the same on every target and in every mode. The same flags run the same package
on either driver:

```sh
unmute dev examples/salon-support --target pipecat \
  --var customer_name=Ada \
  --var customer_id=cus_2002
```

```sh
unmute dev examples/salon-support --target livekit \
  --var customer_name=Ada \
  --var customer_id=cus_2002
```

Quote a value that contains spaces, and pass the whole `name=value` pair inside
the quotes:

```sh
unmute dev examples/outbound-reminder --target pipecat \
  --var customer_id=cus_1042 \
  --var name=Ada \
  --var "appointment_time=tomorrow at 3 pm"
```

`--var` is not web-only. It reaches console and telephony runs too, because the
value is set before the mode is chosen:

```sh
unmute dev examples/salon-support --target pipecat --console --var customer_name=Ada
```

```sh
unmute dev examples/outbound-reminder --target pipecat --telephony \
  --to +15551234567 \
  --var customer_id=cus_1042 --var name=Ada
```

Only `source: call_start` variables can be seeded, and every flag is checked
before anything compiles or starts, so a mistake costs you no container:

- An undeclared name is refused: `--var wrong=1: no variable "wrong" is declared in agent.yaml`.
- A `source: conversation` variable is refused, because the model saves it mid-call: `--var reschedule_to=x: "reschedule_to" has source conversation, so the model saves it mid-call through update_variables, not you`.
- A system source such as `to_number` is refused, because the runtime supplies it.
- A value is parsed against the variable's declared type, so an `integer` variable rejects text instead of handing the bot a quoted string.

Leave a flag out and the variable falls back to its declared `default`. A
`call_start` variable with no default and no `--var` is what the tool guards
send you back to ask for. See [variables](variables.md).

- Web (default) and telephony modes require **Docker** with the Compose plugin.
  Console mode requires `uv` on your `PATH` (see
  [install](../start/install.md)). All modes merge keys from the current
  directory's `.env`, then the package-root `.env`; package values override
  shared values. The compose files list env var names only. Values come from
  your environment at run time and are never written into the file.
- `--port` sets the dev UI port (default 8765). `--bot-port` sets the host port
  the agent container is published on (default 7860); the CLI passes it to
  Compose as `UNMUTE_DEV_PORT` in web mode and `UNMUTE_TELEPHONY_PORT` in
  telephony mode. `--console` ignores both web ports.
- Compose projects use `unmute-<source-dir>-<target>-<path-hash>`, so several
  local stacks coexist. Separate stacks still need distinct host ports;
  LiveKit web publishes `7880` (signal), `7881` (ICE/TCP), and `7882/udp`
  (ICE/UDP mux). Telephony LiveKit additionally accepts `UNMUTE_LIVEKIT_PORT`,
  `UNMUTE_LIVEKIT_SIP_PORT`, and `UNMUTE_LIVEKIT_RTP_PORT_RANGE`.
- `--no-open` skips opening the browser; `--verbose` follows the container logs
  in your terminal.
- Web-mode logs (both targets) are written to `build/<name>/dev.log`; telephony
  logs use `telephony.log`; console mode streams straight to your terminal.
  Press `ctrl-c` to stop; the stack comes down and data volumes are kept.
- Fails clearly if no target is declared, the selected provider has no local
  runner, Docker or its daemon is unavailable for web/telephony mode, a declared
  service is unhealthy, required telephony credentials are missing, local
  LiveKit settings conflict, or `uv` is unavailable for console mode.
