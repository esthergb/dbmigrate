.PHONY: fmt lint test build vulncheck ci ci-manual release-gate-minimal release-gate-full

fmt:
	gofmt -w ./cmd ./internal

lint:
	golangci-lint run ./...

test:
	go test ./... -count=1

build:
	go build -trimpath -ldflags="-s -w" -o bin/dbmigrate ./cmd/dbmigrate

vulncheck:
	govulncheck ./...

ci: fmt lint test vulncheck

ci-manual:
	./scripts/ci_manual.sh "$(or $(BRANCH),$(shell git branch --show-current))"

release-gate-minimal:
	./scripts/run-v1-release-gate.sh --mode minimal

release-gate-full:
	./scripts/run-v1-release-gate.sh --mode full
