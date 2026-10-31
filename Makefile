VERSION ?= dev
LDFLAGS := -X github.com/felipe9962/repsol-cli/internal/cli.Version=$(VERSION)

.PHONY: build test lint live demo clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/repsol ./cmd/repsol

test:
	go test -race ./...

lint:
	go vet ./...
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run; else echo "golangci-lint not found, skipped"; fi

live:
	go test -tags live -run Live -v ./internal/live/

# demo records images/demos/order.gif against the fake backend; needs vhs and jq.
demo:
	scripts/demo/record.sh

clean:
	rm -rf bin dist
