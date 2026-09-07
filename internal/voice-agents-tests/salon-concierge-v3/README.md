# salon-concierge-v3

This test package demonstrates intentional state sharing. It is kept under
`internal/voice-agents-tests` because it is a verification package, not a
starter example.

The normal booking task keeps speech history. It creates a booking or selects
an existing booking and a new slot, then saves five typed variables:
`appointment_id`, `appointment_date`, `appointment_time`,
`appointment_slot_id`, and `appointment_service`.

The slot ID uses `string` to preserve the exact value returned by availability,
including its `|` separators. The concierge explicitly reads the saved service,
date, and time when confirming the outcome.

The rescheduling task uses `history: reset`. Its prompt names only those saved
variables, so it receives the appointment details without receiving the earlier
conversation. `modify_booking` injects the saved customer, booking, service, and
slot values. The model supplies only `confirmed` after reading the change back.

Customer verification shows the same rule. Caller ID is prefetched into
`customer_phone`, and the read-only customer lookup fills `customer_name` and
`customer_id` in one call. The verifier can read those candidate values. Other
prompts cannot use them until the caller agrees and verification saves them.

Task output types come from the destination variables in each task's `assign`
list. There are no separate task result or expected-input declarations.

Validate and compile both targets:

```sh
unmute validate internal/voice-agents-tests/salon-concierge-v3
unmute compile internal/voice-agents-tests/salon-concierge-v3
```

Run the local tools' check:

```sh
python3 internal/voice-agents-tests/salon-concierge-v3/tools/salon.py
```

For browser testing, copy the generated environment example, then run either
target. Seed a caller number when the browser has no carrier:

```sh
unmute dev internal/voice-agents-tests/salon-concierge-v3 \
  --target livekit \
  --source from_number=+34111111111
```

Use `scripts/text_run_livekit.py` for the repeatable text conversation described
in `specs/007-simplify-typed-sharing/quickstart.md`. A real inbound call still
requires the carrier and Langfuse values listed in `.env`.
