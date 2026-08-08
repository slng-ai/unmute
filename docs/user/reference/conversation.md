# Reference: conversation

`conversation` shapes the call's behavior as **outcomes, not provider knobs**. All lifecycle fields gate per target.

```yaml
conversation:
  greeting:
    speaks_first: agent
    text: "Hi, you have reached Acme Support. How can I help you today?"
  interruption:
    enabled: true
    minimum_words: 2
    ignore_phrases:
      - okay
      - right
      - uh-huh
  inactivity:
    nudge_after: 15s
    end_after: 45s
  max_duration: 20m
  thinking_audio: subtle
```

If the whole `greeting` block is absent, the target's own default applies and the driver warns, because provider defaults differ.

## greeting.speaks_first

Who talks first.

Required: yes, if the greeting block is present. Values: `agent | user`. Default: none.

| Target | `agent` | `user` |
|---|---|---|
| LiveKit | core (generated) | generated |
| Pipecat | core (generated) | generated |
| Vapi | native | native |
| Deepgram | core | warns (omission behavior undocumented) |

`user` means the agent stays silent until the caller talks. It is `warn` overall because of Deepgram.

## greeting.text

The exact opening line, spoken word for word every call. May reference `{{variables}}` known at call start.

Required: no. Values: text. Default: none. Targets: all four, core. (Native on Vapi and Deepgram; generated on LiveKit and Pipecat.)

### The three greeting combinations

- **`speaks_first: agent` with `text`**: a fixed opening, same words every call. Works on all four. The zero-warning safe choice.
- **`speaks_first: agent` without `text`**: the model writes the opening from the prompt, so it varies. Generated on LiveKit and Pipecat, native on Vapi, **generated with a warning on Deepgram**.
- **`speaks_first: user`**: the agent waits for the caller. Native on Vapi, generated on LiveKit and Pipecat, warns on Deepgram.

## interruption.enabled

Whether the caller can interrupt the agent.

Required: yes, if the interruption block is present. Values: bool. Default: none. Targets: all four, core.

## interruption.minimum_words

How many words the caller must say before it counts as an interruption.

Required: no. Values: int. Default: none. Tag: warn.

| Target | What happens | Tag |
|---|---|---|
| LiveKit | honored | core |
| Pipecat | honored (minimum-words turn-start strategy) | core |
| Vapi | honored | core |
| Deepgram | lossy (the model halts first); warns | warn |

## interruption.ignore_phrases

Short phrases (like "okay", "uh-huh") that should not count as interruptions.

Required: no. Values: a list of text. Default: none. Tag: warn.

| Target | What happens | Tag |
|---|---|---|
| LiveKit | generated | core |
| Pipecat | generated (emitted as an ignore list) | core |
| Vapi | native | core |
| Deepgram | dropped with a warning | warn |

## inactivity.nudge_after, inactivity.end_after

How long to wait before nudging a silent caller, and before ending a stalled call.

Required: no. Values: durations. Default: none. Tag: warn (each driver range-checks the values per target).

## max_duration

A hard cap on call length.

Required: no. Values: a duration. Default: none. Tag: warn. Some providers have no cap knob, so the driver gates and documents it per target.

## thinking_audio

A subtle sound while the model thinks, so silence does not feel like a dropped call.

Required: no. Values: `none | subtle`. Default: none.

| Target | What happens | Tag |
|---|---|---|
| LiveKit | native | gated |
| Pipecat | native, but the driver does not emit it yet (maturity gate) | gated |
| Vapi | fails (no faithful lowering) | gated |
| Deepgram | fails (no faithful lowering) | gated |

On Pipecat, `thinking_audio` fails validation today; it is a driver maturity gate.
