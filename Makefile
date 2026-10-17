# The same three variables .goreleaser.yaml stamps, so `make build` and a real
# release print the same shape. Nothing here reads the wall clock: `date` is the
# commit date, which keeps repeat builds byte-identical.
VERSION = $(shell git describe --tags --always 2>/dev/null || echo dev)
COMMIT  = $(shell git rev-parse --short HEAD 2>/dev/null)
DATE    = $(shell git log -1 --format=%cI 2>/dev/null)
LDFLAGS = -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

build:   ; CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/unmute .
# -race needs cgo, so this is the one target that does not set CGO_ENABLED=0.
# It found a real ctrl-c race in dev_compose.go the day it was turned on.
test:    ; go test -race ./...
# Exact SDK setup makes the full generator smoke exceed Go's 10m default on a clean cache.
smoke:   ; go test -timeout 20m -tags smoke ./...
# The vendored SLNG conformance fixtures, re-fetched and compared against the
# checked-in digests. Needs network and no credentials, which is why it is opt-in
# and out of the PR gate: story 6 exists to notice the day the two repositories
# drift, and a vendored copy with no refresh check cannot notice anything.
contracts: ; go test -tags contracts -run TestSlngContractsHaveNotDrifted ./internal/generate/
lint:    ; golangci-lint run
# Every example compiled and held to its own generated ruff rules. Needs uv.
lint-emitted: build ; sh scripts/lint_emitted.sh
fmt:     ; gofmt -w . && go vet ./...
install: ; go install -ldflags "$(LDFLAGS)" .
docs:    ; cd docs-site && npx --yes mint dev --no-open

# The exact command the release-config CI job runs, so a maintainer rehearses
# the whole pipeline locally. Publishes nothing; signing needs CI's OIDC token.
# GH_PAT can stay unset: snapshot mode never publishes, so the publishers'
# {{ .Env.GH_PAT }} template is never evaluated (verified 2026-08-14).
release-dry: ; goreleaser release --snapshot --clean --skip=sign

# Every package under internal/voice-agents-tests with a scenarios/ folder,
# compiled for each code target and run as whole calls on the real model by
# unmute-core's runner. Needs uv, network and the keys in .env, so it is out of
# the PR gate. PKG and FW narrow it: make e2e PKG=kestrel FW=pipecat.
# UNMUTE_E2E_CORE=../unmute-core runs an unreleased unmute-core checkout.
e2e:     ; go test -tags e2e -timeout 60m -count=1 ./internal/e2e/ -run 'TestScenarios$(if $(PKG),/$(PKG)$(if $(FW),/$(FW)))' -v

.PHONY: build test smoke e2e contracts lint lint-emitted fmt install docs release-dry
