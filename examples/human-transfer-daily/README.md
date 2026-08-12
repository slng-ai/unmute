# human-transfer-daily

A cold transfer on Pipecat, on the one route where Pipecat has a native
primitive: Daily. The bot announces the handoff, calls
`transport.sip_call_transfer`, Daily reroutes the caller's leg, and the bot
drops off.

Warm transfer is not here on purpose: Pipecat has no native warm transfer,
and the only documented pattern makes the bot own the audio path, which this
project deleted. Warm lives in the LiveKit example
([human-transfer](../human-transfer)); the capability map with sources is in
[docs/TRANSFERS.md](../../docs/TRANSFERS.md).

## What it needs

- A Daily domain with a purchased phone number and **dial-out enabled**
  (`sip_call_transfer` requires it).
- Pipecat Cloud to run the bot and connect the number to it with its managed
  dial-in webhook, or your own Daily room wiring.
- The destination as an env var, resolved at call time:

```sh
DAILY_API_KEY=...
BILLING_PHONE_NUMBER=+1...
```

If the transfer fails, the failure is a return value on this transport, not
an exception. The generated tool reads it and applies `on_unavailable`: by
default the agent tells the caller and keeps helping; `hangup` says a goodbye
line and ends the call.

## Run it

```sh
bin/unmute validate examples/human-transfer-daily
```

```sh
bin/unmute compile examples/human-transfer-daily
```

That writes `build/pipecat/`. Its own `README.md` has a Deploy section printing
the exact commands: create the secret set from `.env` first, because the emitted
`pcc-deploy.toml` already names it, then `pipecat cloud deploy`, which builds the
image in the cloud from the emitted `Dockerfile`. After that, connect the Daily
number in the Pipecat Cloud dashboard, then call the number and ask about an
invoice. The full walkthrough, including teardown, is in
[docs/TRANSFERS.md](../../docs/TRANSFERS.md).
