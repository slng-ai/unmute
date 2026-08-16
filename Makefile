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
smoke:   ; go test -tags smoke ./...
lint:    ; golangci-lint run
fmt:     ; gofmt -w . && go vet ./...
install: ; go install -ldflags "$(LDFLAGS)" .
docs:    ; cd docs-site && npx --yes mint dev --no-open

# The exact command the release-config CI job runs, so a maintainer rehearses
# the whole pipeline locally. Publishes nothing; signing needs CI's OIDC token.
# GH_PAT can stay unset: snapshot mode never publishes, so the publishers'
# {{ .Env.GH_PAT }} template is never evaluated (verified 2026-08-14).
release-dry: ; goreleaser release --snapshot --clean --skip=sign

.PHONY: build test smoke lint fmt install docs release-dry
