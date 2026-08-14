# The same three variables .goreleaser.yaml stamps, so `make build` and a real
# release print the same shape. See specs/010-goreleaser-release-pipeline/
# contracts/version-output.md. Nothing here reads the wall clock: `date` is the
# commit date, which keeps repeat builds byte-identical.
VERSION = $(shell git describe --tags --always 2>/dev/null || echo dev)
COMMIT  = $(shell git rev-parse --short HEAD 2>/dev/null)
DATE    = $(shell git log -1 --format=%cI 2>/dev/null)
LDFLAGS = -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

build:   ; CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/unmute .
test:    ; go test ./...
smoke:   ; go test -tags smoke ./...
lint:    ; golangci-lint run
fmt:     ; gofmt -w . && go vet ./...
install: ; go install -ldflags "$(LDFLAGS)" .
docs:    ; cd docs-site && npx --yes mint dev --no-open

.PHONY: build test smoke lint fmt install docs
