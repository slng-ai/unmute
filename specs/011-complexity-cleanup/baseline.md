# Baseline: Complexity Cleanup

**Recorded**: 2026-08-15 | **Tasks**: T001, T003, T004, T005

Everything this feature promises is measured against the numbers on this page.
They were taken before the first line of cleanup, which is the only time they
mean anything (FR-001).

---

## Commit

| Field | Value |
|---|---|
| Baseline sha | `1085102` |
| Branch | `main` (worktree `.claude/worktrees/cleanup`) |
| Tree state | clean apart from untracked `specs/011-complexity-cleanup/` |

## Tooling (T001)

| Tool | Version | Note |
|---|---|---|
| Go toolchain | `go1.26.4 darwin/arm64` | module pins `go 1.24`; `stdversion` caps the stdlib surface at 1.24 |
| golangci-lint | `2.12.2` | |
| ruff | **`0.15.14`** | **load-bearing** — every byte comparison must use this exact version (research.md R3) |
| Docker | `29.5.3`, daemon reachable | |
| uv | `0.11.16` | L4 smoke available |
| Node | `v24.16.0` | |
| Playwright | **absent** | `npx playwright` refuses to install unattended; see Phase 2 notes |

## Gate (T003)

Green at the baseline commit:

```text
make fmt     → gofmt -w . && go vet ./...   (clean)
make lint    → 0 issues
make build   → bin/unmute, v0.1.2-2-g1085102
make test    → all 9 packages ok
```

## Metrics (T005)

| Metric | Baseline | Source |
|---|---|---|
| Non-test Go lines | **20,901** | `find internal main.go -name "*.go" -not -name "*_test.go" \| xargs wc -l` |
| Unreachable functions | **2** | `deadcode -test ./...` |

The two unreachable functions are exactly the ones the audit named:

```text
internal/style/style.go:72:6: unreachable func: Warned
internal/style/style.go:75:6: unreachable func: Ok
```

## Compiled baseline tree (T004)

All eleven examples compile clean. The tree lives **outside the repository**, in
the session scratch directory, so it can never be committed:

```text
<scratch>/baseline/<example>/<target>/
```

**19 target directories, 243 files.** Eleven examples, and every example
declares both `livekit` and `pipecat` except four that declare one:

| Example | livekit | pipecat | Browser-runnable |
|---|:--:|:--:|:--:|
| mcp-example | ✓ | ✓ | both |
| multi-task | ✓ | ✓ | both |
| salon-support | ✓ | ✓ | both |
| simple-prompt | ✓ | ✓ | both |
| subagents | ✓ | ✓ | both |
| task-groups | ✓ | ✓ | both |
| pipecat-human-transfer-daily | — | ✓ | pipecat |
| livekit-human-transfer | ✓ | — | compile only |
| outbound-reminder | ✓ | ✓ | compile only |
| pipecat-human-transfer-twilio | — | ✓ | compile only |
| twilio-telephony-hello | ✓ | ✓ | compile only |

**Thirteen browser sessions**, which is the figure SC-008 uses. The four
compile-only examples carry a telephony channel and are excluded by FR-031.

## Secrets (T002, FR-038)

The root `.env` holds **27 assignments**. Every name the thirteen runnable
sessions reference is present:

```text
BILLING_PHONE_NUMBER  FIRECRAWL_MCP_URL  OPENAI_API_KEY  SLNG_API_KEY
```

`FIRECRAWL_MCP_URL` was the open precondition in FR-038; it is now supplied, so
`mcp-example` no longer fails under FR-035.

Values are never recorded here, in any sweep report, or in any log (FR-036,
SC-011). Names only.
