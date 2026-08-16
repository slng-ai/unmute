# Contract: Run Commands

**Feature**: 016-upgrade-target-runtimes | **Date**: 2026-08-16

The exact commands emitted artifacts and `unmute dev` run, before and after.
Research R7 and R8 establish why; this file is what the goldens and tests pin.

## 1. LiveKit

| Site | Today | After |
|---|---|---|
| `compose.dev.yaml` service command | `["python", "agent.py", "dev"]` | `["python", "-m", "livekit.agents", "start", "agent.py", "--log-format", "colored"]` |
| `Dockerfile` `CMD` | `["python", "agent.py", "start"]` | `["python", "-m", "livekit.agents", "start", "agent.py"]` |
| `compose.telephony.connector.yaml` command | `sh -c "python agent.py start & exec python telephony_bridge.py"` | `sh -c "python -m livekit.agents start agent.py & exec python telephony_bridge.py"` |
| `compose.telephony.yaml` (SIP) | no command, inherits `CMD` | unchanged, inherits the new `CMD` |
| Emitted `README.md` | `uv run agent.py console`, `uv run agent.py dev`, `python agent.py start` | one worker command: `python -m livekit.agents start agent.py` |
| `agent.py` tail | `if __name__ == "__main__": cli.run_app(server)` | removed, with the now-unused `cli` import |

Rules:

- No emitted file, compose service, or document may name `agent.py dev`,
  `agent.py console`, or `cli.run_app`. All three are deprecated at the ceiling
  version (R7), and the first two print visible warnings on every run.
- `--log-format colored` appears only in the dev compose, because a human is
  watching that output. Production paths keep the JSON default.
- `internal/target/telephony.go`'s connector process command and the connector
  template must stay byte-identical to each other; the existing telephony
  agreement test covers this.
- The module-level `server = AgentServer(...)` stays exactly as it is. It is
  what makes the thin CLI able to discover the agent at all (R7).

## 2. Pipecat

| Site | Today | After |
|---|---|---|
| `compose.dev.yaml` service command | `["python", "bot.py", "--host", "0.0.0.0", "--port", "7860"]` | unchanged |
| `Dockerfile` (telephony) `CMD` | `["uvicorn", "telephony:app", ...]` | unchanged |
| `bot.py` entrypoint | `if "console" in sys.argv[1:]: asyncio.run(console_main())` else `main()` | `main()` only; `console_main()` deleted |
| `pyproject.toml` | `console = ["pipecat-ai[local]=={{.Version}}"]` | group deleted |
| Emitted `README.md` | includes `uv run --extra console bot.py console` | console line deleted |

Pipecat's own run modes are not deprecated. Its console scaffolding goes only
because `unmute dev --console` goes (FR-013).

## 3. The `unmute dev` command

| Aspect | Today | After |
|---|---|---|
| Browser (default) | Docker compose, both targets | unchanged |
| Terminal | `--console`, uv, no Docker | **removed** |
| Telephony | `--telephony`, compose | unchanged |
| Docker missing | error offers `--console` as a fallback | error states Docker is required, with the install hint and no fallback |
| `--console` passed | runs a terminal session | fails with a clear error naming browser dev mode, never a bare unknown-flag message |

The removed-flag error must explain rather than merely reject, because authors
have the old invocation in their shell history and in older documentation:

```text
dev: --console was removed; local development runs in Docker.
Run `unmute dev <package>` to talk to the agent in your browser.
```

## 4. What must not change

- Explicit token dispatch for the browser path
  (`roomConfig.agents[].agentName`). It is what makes the agent join, it is
  independent of the run mode, and it is why the run-command change is safe
  (R8).
- The `"registered worker"` readiness marker the browser path waits for. It
  comes from worker registration, which both the old and new commands perform.
- Compose verbs and flags: `up --build --detach --remove-orphans --wait`,
  `logs --follow --no-color`, `down --remove-orphans --timeout 30`, and never
  `--volumes`.
- The local single-node `livekit-server` container and its dev credentials. No
  local path talks to LiveKit Cloud.
