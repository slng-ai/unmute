# telephony-multi-task

The multi-task salon agent with a phone channel. One agent delegates
customer records and appointment work to tasks, answers inbound calls, and
can cold-transfer the caller to a person.

It declares two routes for the same agent:

- `pipecat`: Twilio Programmable Voice over `carrier-websocket`. Needs
  `TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, `TWILIO_PHONE_NUMBER`.
- `livekit`: Twilio Elastic SIP Trunking into the self-hosted LiveKit SIP
  bridge over `sip`. Needs `TWILIO_SIP_ADDRESS`, `TWILIO_SIP_USERNAME`,
  `TWILIO_SIP_PASSWORD`, `TWILIO_PHONE_NUMBER`.

Copy `.env.example` to `.env` and fill in the values, then run one route:

```sh
unmute dev . --target pipecat --telephony
```

The dev command supplies `UNMUTE_PUBLIC_URL` (managed cloudflared tunnel),
`REDIS_URL`, the local LiveKit key pair, and the LiveKit trunk IDs itself.
The Pipecat route needs `cloudflared` on PATH (macOS:
`brew install cloudflared`), or pass `--public-url` with your own tunnel.

Honest status: every telephony route is still provisional, so `unmute
validate`, `unmute compile`, and `unmute dev --telephony` fail closed on
this package today. The configuration is real and complete; it runs once
its exact route passes the credentialed call smokes. See
[07. Phone calls](../../docs/user/learn/07-phone-calls.md).
