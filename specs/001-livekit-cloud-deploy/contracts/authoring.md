# Contract: what an author may write

Applies to `targets.yaml`. The field name and its optionality are unchanged; only the accepted shape grows. Lands as `docs/SCHEMA.md` amendment N32.

## Accepted shapes

```yaml
targets:
  livekit:
    provider: livekit
    deployment_region: us-east            # one region, unchanged from N18
```

```yaml
targets:
  livekit:
    provider: livekit
    deployment_region:                    # several regions, new in N32
      - us-east
      - eu-central
```

```yaml
targets:
  pipecat:
    provider: pipecat
    # deployment_region omitted: the platform's own default applies
```

## Rules

| Rule | Behaviour |
|---|---|
| Optional on every provider | Omitting it is valid. No region is ever invented; each generated README states what that platform does instead. |
| One region, any provider | Valid. A one-element list behaves identically to the scalar form, including the file names in the printed commands. |
| Several regions, `livekit` | Valid. One deployment per region from one build directory. |
| Several regions, `pipecat`, `vapi`, `deepgram` | **Gated error** before any artifact is written, quoting that platform's own reason. Pipecat's: agent names are globally unique across regions, so a second region needs a differently named agent. |
| Duplicate region in a list | Error. Never deduplicated silently. |
| Empty string in a list | Error. |
| Region code | Never validated and never enum-checked. Forwarded exactly as written; the platform CLI is the validator (N18, unchanged). No region code list is kept in this repository, because both platforms change theirs without notice. |
| Existing packages | A package written against N18 keeps loading with no edit. That is the reason the field kept its name. |

## What does not become an author's field

Anything a platform assigns: a LiveKit project subdomain, a LiveKit Cloud agent ID, a Pipecat deployment identity. These reach a build directory only from the platform's own CLI, never from a package and never from an emitter.
