# Quickstart: Verify a Spoken Round-Trip Handoff

## Compile

```sh
make build
bin/unmute validate examples/subagents
bin/unmute compile examples/subagents
```

Read both generated transfer methods. The requirement guard must appear before
the announcement, and the announcement before target activation or return.

## Run both targets

```sh
bin/unmute dev examples/subagents --target livekit --no-open
bin/unmute dev examples/subagents --target pipecat --port 8766 --bot-port 7861 --no-open
```

## Speak the same script to each

1. "I want to change an appointment I already have."
2. Give `+1 555 010 101` when asked.
3. "Actually, leave it unchanged. I want a separate new haircut appointment
   for August eighteenth, twenty twenty-six."
4. Choose the returned 3 p.m. slot.

Pass means:

- one opening greeting for the whole call;
- one natural cue before each handoff;
- no agent speaks over a cue;
- the phone number is requested once;
- `lookup_customer`, the two transfers, `check_availability`, and
  `book_appointment` run in order;
- booking uses the exact returned `customer_id` and `slot_id`;
- no `TypeError`, provider error, `ErrorFrame`, traceback, or duplicate transfer.

Repeat five times on each target for the feature success criteria.
