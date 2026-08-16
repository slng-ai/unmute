# Contract: `agent_transfer` announcement

```yaml
controls:
  to_appointment_manager:
    kind: agent_transfer
    to: appointment_manager
    when: The caller wants to change an existing appointment.
    announce: "I’m connecting you with our appointment manager now."
    requires:
      - customer_id
    context:
      history: full
      variables: all
```

Observable order:

1. Check `requires` and refuse without speech when it fails.
2. Speak the exact `announce` sentence once.
3. Finish that cue.
4. Change the active agent.
5. Let the receiving agent continue from the selected context.

With no `announce`, step 2 is absent. Returning to `entry_agent` never replays
the call-start greeting.

Invalid forms:

```yaml
announce: ""
```

```yaml
announce: "Tell {{name}} about the handoff."
```

```yaml
kind: delegate
announce: Tell the caller about the handoff.
```
