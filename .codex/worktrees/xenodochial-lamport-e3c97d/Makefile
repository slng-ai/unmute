LDFLAGS = -s -w -X main.version=$(shell git describe --tags --always 2>/dev/null || echo dev)

build:   ; CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/unmute .
test:    ; go test ./...
smoke:   ; go test -tags smoke ./...
lint:    ; golangci-lint run
fmt:     ; gofmt -w . && go vet ./...
install: ; go install -ldflags "$(LDFLAGS)" .

.PHONY: build test smoke lint fmt install
