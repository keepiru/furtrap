.PHONY: build clean test

COMMIT := $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +%FT%TZ)

LDFLAGS := -X 'main.buildGitCommitHash=$(COMMIT)' -X 'main.buildTimestamp=$(BUILD_DATE)'

build:
	go build -ldflags="$(LDFLAGS)" -o furtrap

clean:
	rm -f furtrap

test:
	go test ./...
