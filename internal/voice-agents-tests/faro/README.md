# faro

A bilingual language tutor on the phone, ported from unmute-core's `examples/faro`.
Faro knows learners by their number, signs new ones up, and teaches short lessons in
English or Spanish. It explains in the learner's language, puts each exercise, grades
the answer, and carries on from where they stopped, across calls.

The model grades and code teaches. Nearly every turn of a lesson is one model request.

## What it proves

- Two agents, `faro_en` and `faro_es`, one per explanation language, with a top-level
  `handoffs:` catalog between them.
- `from_number` prefetch, then a tool prefetch (`language_of`) that picks the greeting's
  language and number read-back before anything is said.
- A greeting built from state, with an `instruction` fallback when there is no caller ID.
- A lesson with no task: agent tools whose `said` result is spoken by an `outcomes:` entry
  with `save:` where a result is kept.
- `with:` keeps the learner, the language and the exercise waiting out of the model's hands.
- `precheck` and an outcome that refuse a number from outside Spain, in the caller's language.
- Two tasks that read back before acting: `other_number` (`arguments`, `start:` with
  `call:`, `with:`, `say:` and `listen: true`) and `sign_up` (a `confirm` of a new profile).
- Progress in SQLite, so it survives a call and a restart.
- Typed tools: `Literal` grades and levels, `Phone`, `ToolError`.

## The learners

The store is made with three learners the first time. Any other Spanish number is new.

| Number | Name | Explains in | Learns |
|---|---|---|---|
| +34 600 111 222 | Lucía | Spanish | English for travel |
| +34 600 333 444 | Diego | Spanish | English for work |
| +34 600 555 666 | Emma | English | Spanish for daily life |

## FARO_DB

`tools/lessons.py` keeps learners in the SQLite file named by `FARO_DB`, or
`faro.sqlite3` in the working folder. It is a path, not a secret, so it is not under
`secrets:`. The compiler warns that it is not declared; that is expected.

The e2e run sets `FARO_DB=:memory:`, as the unmute-core runner script does, so each run
starts from the three seeded learners:

```bash
FARO_DB=:memory: python -I -m unmute_core.testing converse livekit agent --patch voice_stack scenarios/*.json
```

## Layout

| Path | What it holds |
|---|---|
| `agent.yaml` | Both agents, their tasks, the handoffs, prefetch, models |
| `state.py` | The call's `State` |
| `tools/lessons.py` | The lessons, the SQLite store and every tool (one module) |
| `tools/*.yaml` | One file per tool: with, precheck. Its save and outcomes sit on the tool's entry in `agent.yaml` |
| `instructions.md`, `instructions-es.md` | The prompt, once per agent |
| `tasks/*.md` | The prompts of the two tasks |
| `scenarios/*.json` | unmute-core's scenarios, unchanged |

## Gaps

The call starts on the agent of the caller's own language, as in core, through `entry_by:` on `explanation`.

What core does that v2 cannot say yet, and what is written instead.

- **Agent timezone.** Core sets `Europe/Madrid` on each agent. The package has no
  clock prefetch, so its agents have no timezone. No line names a date. Each agent's
  language is its voice's: `en` for `faro_en`, `es` for `faro_es`, as in core.
- **The same task in two agents.** Core builds one task from a template per language.
  A task name is one name across the package, so the Spanish agent's are `other_number_es`
  and `sign_up_es`, and its prompt, `instructions-es.md`, names them.
- **`said` and the read-backs.** Core's replies say `{{result.said}}` and its read-backs
  say `{{input.phone}}` and `{{input.name}}`. So do these: an outcome `say:` reads
  `{{result.said}}` and a `confirm:` line reads `{{input.x}}`, so `State` holds no
  `said` and no `profile_*` field.
- **The greeting's read-back.** Core builds `number_question` in Python after the prefetch.
  Here `language_of` returns it, and the prefetch assigns it.
- **Task-only tools.** Core's `task_only_tools`: `check_number` and `create_profile` are
  listed under the tasks only, and `tools:` at the top lists every tool.
