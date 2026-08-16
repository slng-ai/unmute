# Contract: Version Support Surface

**Feature**: 016-upgrade-target-runtimes | **Date**: 2026-08-16

What an author sees. Message wording may be refined during implementation, but
each message must carry the facts listed as required, because those are what the
acceptance scenarios check.

## 1. The supported window

| Framework | Floor | Ceiling | Verified |
|---|---|---|---|
| `pipecat-ai` | 1.5.0 | 1.7.0 | 2026-08-16 |
| `livekit-agents` | 1.5.0 | 1.6.10 | 2026-08-16 |

The ceiling is a claim about human verification, not about upstream. A newer
upstream release is unsupported until a human verifies it and a new unmute
ships.

## 2. Validation errors

All are gated: they fail before any artifact is written, and with several
resolved targets, one failure fails the run.

**Below the floor** (existing behavior, message updated to name the window):

```text
pipecat version "1.4.9" is outside the supported range (>=1.5.0, <=1.7.0)
```

Required facts: the provider, the offending version, both bounds.

**Above the ceiling** (new):

```text
livekit version "1.7.0" is newer than this unmute supports (>=1.5.0, <=1.6.10);
a newer unmute may support it
```

Required facts: the provider, the offending version, both bounds, and that
upgrading unmute is the fix. FR-004 requires the last part; without it the
author cannot tell an unsupported version from a typo.

**Not a full version** (new, R6):

```text
livekit version "1.6" must name all three parts, for example "1.6.10"
```

**Feature floor unmet** (new, R4). One message per unmet floor:

```text
livekit target "main" declares version "1.5.2", but a warm transfer needs
livekit-agents >=1.6.0
```

```text
livekit target "main" declares version "1.5.2", but an MCP tool source needs
livekit-agents >=1.6.0
```

Required facts: the target name, the declared version, the feature, and the
floor. The feature must be named in the package's own vocabulary (a warm
transfer, an MCP tool source), not as an internal flag name.

**Missing version** (existing, unchanged):

```text
livekit code target requires version
```

## 3. Emitted dependency pins

**LiveKit** (`pyproject.toml`), the change. Extras still merge onto the package;
the constraint is now the declared version exactly:

```toml
dependencies = [
    "livekit-agents[openai]==1.6.10",
    "livekit-plugins-silero>=1.6.1",
    "livekit-plugins-slng>=1.6.1",
    "httpx",
    "python-dotenv",
]
```

Rules:

- The framework constraint is `==<declared version>`. No `>=`, no upper bound,
  no feature-driven variation.
- Extras are unchanged: a feature that needs an extra still adds it (`mcp` is
  the live case), because which extra to install is a different fact from which
  version to install.
- Plugin packages keep their own floors, and a user `pins:` entry still raises a
  floor. The silero floor is read from its single home rather than a literal.

**Pipecat** (`pyproject.toml`), unchanged in shape, new default version:

```toml
dependencies = [
    "pipecat-ai[deepgram,openai,runner,silero,webrtc]==1.7.0",
]
```

The `console` optional-dependency group is deleted (FR-013).

## 4. Compile report

The report gains the window alongside the version it already records, so an
author can see what their unmute supports without opening a document (FR-007):

```json
{
  "target": {
    "provider": "livekit",
    "version": "1.6.10",
    "supported": { "floor": "1.5.0", "ceiling": "1.6.10", "verified": "2026-08-16" }
  }
}
```

Field names are indicative; the requirement is that floor, ceiling, and
verification date are present per code target.

## 5. Version output

`unmute --version` also states the window, one line per framework, so the range
is discoverable without compiling anything:

```text
unmute v0.1.3 (abc1234 2026-08-16)
supported frameworks: pipecat-ai 1.5.0-1.7.0, livekit-agents 1.5.0-1.6.10 (verified 2026-08-16)
```

The existing first line keeps its shape, which
`specs/010-goreleaser-release-pipeline/contracts/version-output.md` pins.

## 6. Scaffold default

`unmute init` writes the ceiling for the chosen target:

```yaml
targets:
  - provider: pipecat
    version: "1.7.0"
```

```yaml
targets:
  - provider: livekit
    version: "1.6.10"
    sdk_language: python
```

## 7. Invariants the agreement test holds

1. The recorded window is non-empty, and `floor <= ceiling` for every code
   target. A vacuous table fails the test rather than passing it.
2. No author-facing surface names a framework version that disagrees with the
   window: examples, `docs/`, `docs-site/`, the skill bundle, the scaffold
   default, and `README.md`.
3. Carve-outs, documented in the test: goldens, `internal/testdata/` fixtures,
   and `specs/` history, which legitimately record older versions.
4. The emitted LiveKit constraint equals `==` plus the declared version, for
   every example and fixture.
