---
name: dbmigrate-code-quality
description: Implement structured logging, real concurrency in data copy, performance patterns, and observability improvements. Use when working on cross-cutting quality improvements that span multiple packages without adding new migration features.
---

# dbmigrate code quality

## Objective

Improve cross-cutting quality areas identified in the v1 review without changing migration semantics.

## Area 1: Structured JSON logging

### Current state

All output goes through `fmt.Fprintf(out, ...)` in commands. There is no configurable logger. The `--verbose` flag is parsed but has no effect beyond some conditional output.

### Implementation plan

1. Create `internal/log/` package with a minimal structured logger:
   - Levels: debug, info, warn, error.
   - JSON output mode (for `--json` flag).
   - Text output mode (default).
   - Thread-safe via `sync.Mutex`.
   - No external dependency — use `encoding/json` + `io.Writer`.

2. Logger interface:

   ```go
   type Logger interface {
       Debug(msg string, fields ...Field)
       Info(msg string, fields ...Field)
       Warn(msg string, fields ...Field)
       Error(msg string, fields ...Field)
   }
   ```

3. Wire logger through `RuntimeConfig` and pass to internal packages.

4. Replace `fmt.Fprintf` debug/info output in internal packages with logger calls.

5. Keep command result output (`writeResult`) unchanged — logger is for operational output, not command results.

### Key constraints

- Do not add external logging dependencies (no zap, zerolog, slog).
- Go 1.21+ `log/slog` from stdlib is acceptable if the project minimum Go version permits.
- Logger must be injectable for testing (interface, not concrete type).

## Area 2: Real concurrent data copy

### Current state

`internal/data/copy.go` processes tables sequentially. The `--concurrency` flag is parsed in `RuntimeConfig` but not used by `CopyBaselineData`.

### Implementation plan

1. Add a worker pool pattern in `CopyBaselineData`:

   ```go
   sem := make(chan struct{}, opts.Concurrency)
   var wg sync.WaitGroup
   var mu sync.Mutex
   var firstErr error
   ```

2. Each table copy runs in a goroutine guarded by the semaphore.

3. Checkpoint updates must be serialized (mutex-protected).

4. Error handling: on first error, drain remaining work and return.

5. Source connection pool: use `source.SetMaxOpenConns()` to match concurrency.

6. Destination connection pool: similarly sized.

7. Consistent snapshot constraint:
   - The current implementation uses a single pinned source connection with `START TRANSACTION WITH CONSISTENT SNAPSHOT`.
   - For concurrent reads, each goroutine needs its own connection with the same snapshot.
   - Use `source.Conn(ctx)` per goroutine, each starting a consistent snapshot transaction.
   - Note: this gives per-connection snapshots, not a global consistent snapshot. Document this tradeoff.

### Key constraints

- Default `--concurrency=4` must not break existing sequential behavior guarantees.
- Checkpoint must remain crash-safe (atomic writes via `internal/state`).
- Each goroutine must use its own `database/sql.Conn` — never share connections.

## Area 3: Progress reporting

### Current state

No progress indicators. Long-running migrations produce no output until completion.

### Implementation plan

1. Add a progress reporter in data copy:
   - Report per-table: table name, rows copied, total estimate, elapsed time.
   - Report overall: tables done / total, total rows, throughput (rows/sec).

2. Use logger (Area 1) for progress output.

3. For non-TTY output (CI, pipes): emit periodic JSON progress events.

4. For TTY output: emit overwriting progress line (carriage return).

5. Frequency: at most once per second or per chunk, whichever is less frequent.

## Area 4: Rate limiting

### Current state

No rate limiting. Migrations can overwhelm source or destination.

### Implementation plan

1. Add `--rate-limit` flag (rows per second, 0 = unlimited).

2. Implement token bucket or simple sleep-based throttle per chunk.

3. Apply in both data copy and replication apply paths.

## Testing strategy

- Unit tests for logger output formatting.
- Unit tests for concurrent data copy with mock DB connections.
- Benchmark tests for throughput measurement.
- Integration tests verifying concurrent copy produces same results as sequential.

## Linting requirements

All changes must pass the existing `.golangci.yml` linters:
- errcheck
- govet
- staticcheck
- ineffassign

Run `go test ./... -count=1 && go vet ./...` before every commit.
