# Quickstart: validating the extension end to end

Run from the worktree root. Together these prove the feature without reading
the implementation. Sections 2 to 9 assume section 1 is done.

## 1. The merge is in and the tree is healthy (FR-027)

```bash
git log --oneline -1 --merges          # shows the merge of origin/pre-release-v1
make build                             # ./bin/unmute from the merged tree
go test ./...                          # green, including the agreement tests
make lint                              # 0 issues
gofmt -l internal/                     # empty
```

## 2. The moved fields are gone from the site (FR-030, SC-012)

```bash
grep -rn "transport:" docs-site --include="*.mdx"     # judge each hit: none may sit in a targets.yaml snippet
grep -rn "carrier:" docs-site --include="*.mdx"       # same rule
grep -rn "destinations:" docs-site --include="*.mdx"  # only as agent.yaml top level, env names only
grep -rn "kind:" docs-site --include="*.mdx"          # only under agent.yaml channels, never in a connection file
```

Expected: hits exist (connection files legitimately show `transport:` and
`carrier:`), but zero hits inside a `targets.yaml` snippet.

## 3. Every example is ready, including the new one (FR-028, SC-013)

```bash
for e in examples/*/; do ./bin/unmute validate "$e" && ./bin/unmute compile "$e" && ls "$e/README.md" >/dev/null || echo "FAIL $e"; done
go test ./internal/generate -run TestExamples   # the merged version of the README/link rules
```

Expected: all eleven pass all three checks.

## 4. The new pages hold their anchors (SC-013, SC-014, SC-019)

Spot-check: `build/tools/mcp.mdx` cites only `examples/mcp-example` behavior;
copy its illegal-field snippet into a scratch package and confirm the refusal
matches the page word for word. `reference/connections-yaml.mdx` shows three
shapes; validate each shape's package. The quickstart transcript: run
`unmute init` with the page's inputs and diff against the page (SC-015).

## 5. The agreement tests hold the twice-stated facts (FR-037, SC-017, SC-021)

```bash
go test ./internal/cli -run TestHelpCapture      # help.txt still matches; flag mapping moved with any moved page
go test ./internal/target -run ProvidersDocsSite # catalog ↔ models/{stt,tts,llm,turn-detection}.mdx, SLNG first
go test ./internal/spec -run ToolsDocsSite       # execution blocks ↔ build/tools/overview.mdx (new, research R17)
```

Expected: pass; each must fail when its page is edited to lie (prove once,
restore).

## 6. Models and optimization are honest (SC-021, SC-022, SC-023)

```bash
grep -rin "context router\|context-router" docs-site   # zero hits (SC-022)
ls docs-site/reference/providers.mdx 2>&1              # No such file
grep -rn "reference/providers" docs-site               # zero inbound links
```

Manual: each execution-layer claim on `models/optimization.mdx` carries a
link to docs.slng.ai and appears in the verification-log addendum with a
fetch date; the STT layer's Private Beta status is stated as SLNG's own.

## 7. The lifecycle, go-live, and Twilio pages match reality (SC-024 to SC-027)

```bash
grep -rn "11LABS" docs-site                            # zero hits (SC-025)
grep -rn "dev --telephony" docs-site/telephony         # no walkthrough left, a link at most
```

Manual: run each documented dev mode once and check the page's log-file
locations against what appeared (SC-024). For each platform guide, every
command was run or carries a dated attribution plus an unverified-list entry
(SC-026). The telephony/twilio env-name mapping matches the telephony
examples' connection files (SC-027).

## 8. The site is sound and matches disk (FR-037, SC-016)

```bash
cd docs-site
mint validate
mint broken-links
mint dev --no-open        # probe a moved page and a new page by URL
cd ..
find docs-site -name "*.mdx" | wc -l                      # 49
python3 -c "import json;d=json.load(open('docs-site/docs.json'));f=lambda ps:sum(f(p['pages']) if isinstance(p,dict) else 1 for p in ps);print(sum(f(g['pages']) for g in d['navigation']['groups']))"   # 49
grep -rn "—\|–" docs-site        # empty
grep -rin "vapi" docs-site       # nothing presenting Vapi as a target
```

## 9. The addenda exist and are honest (FR-038, SC-018, SC-020)

Read `specs/008-mintlify-user-docs/`: `contracts/navigation.md` ends with a
dated amendment recording the restructure and the new groups;
`verification-log.md` gained a dated addendum naming scratch packages,
captures, and external fetch dates; `report.md` gained an addendum with the
49-page list, a verdict per D1 to D5 (stands, stale, or changed, with
evidence), any new discrepancies, and the unverified claims including any
unexecuted cloud-deploy steps; `tasks.md` gained a new dated phase with items
marked `[X]`. `docs-site/README.md` reflects the rule and test-list changes.
Confirm the docs site deployed nowhere: no Mintlify project, no URL (SC-020).
