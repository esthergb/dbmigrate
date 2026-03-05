.PHONY: fmt lint test build vulncheck ci ci-manual

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
