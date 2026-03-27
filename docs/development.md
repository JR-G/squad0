# Development

## Tech Stack

- **Language**: Go (see `go.mod` for current version)
- **Database**: SQLite via `mattn/go-sqlite3` (CGo) with FTS5 for full-text search
- **Slack**: `slack-go/slack` with socket mode
- **CLI**: `spf13/cobra`
- **Config**: `BurntSushi/toml`
- **Testing**: `stretchr/testify`
- **TUI**: `charmbracelet/bubbletea` + `lipgloss` for the terminal dashboard
- **JWT**: `golang-jwt/jwt/v5` for GitHub App authentication
- **Embeddings**: Ollama (`nomic-embed-text`) — local, free, offline
- **Build**: Task (taskfile.dev) — requires `-tags sqlite_fts5` for all build and test commands

## Building

```bash
# Both binaries
task build

# Or manually:
go build -tags sqlite_fts5 -o bin/squad0 ./cmd/squad0
go build -tags sqlite_fts5 -o bin/squad0-memory-mcp ./cmd/squad0-memory-mcp

# Run
task start          # build + run
task restart        # rebuild, kill existing process, start fresh
task stop           # kill running process
```

## Testing

```bash
# All tests with race detector
task test
# Or: go test -race -tags sqlite_fts5 -v ./...

# Coverage report
task cover
# Or: go test -race -tags sqlite_fts5 -coverprofile=coverage.out ./...
```

### Coverage Target

**95% minimum** — enforced by the `pre-push` hook via `scripts/check-coverage.sh`. Write tests alongside code during development, not after. If coverage is below 95%, the push is rejected.

### Testing Patterns

- **Table-driven tests** as the default pattern
- `t.Parallel()` on all independent tests
- Test names: `TestFunctionName_Scenario_ExpectedBehaviour`
- In-memory SQLite (`:memory:`) for fast database tests
- `httptest` for HTTP API mocking (Slack, Ollama, GitHub)
- Interface-driven design for all external dependencies
- `*_test.go` files live alongside production code
- Test files also have the 500-line limit — split by concern into multiple test files (e.g. `monitor_test.go`, `monitor_coverage_test.go`)

## Code Standards

### Control Flow

**No nested ifs.** Guard clauses and early returns only. Enforced by the `nestif` linter with `min-complexity: 1`. Prefer `switch` over `if-else` chains.

```go
// Wrong
func processTicket(ticket *Ticket) error {
    if ticket != nil {
        if ticket.Status == "Ready" {
            // do work
        }
    }
    return nil
}

// Correct
func processTicket(ticket *Ticket) error {
    if ticket == nil {
        return ErrNilTicket
    }
    if ticket.Status != "Ready" {
        return fmt.Errorf("ticket %s has status %s, expected Ready", ticket.ID, ticket.Status)
    }
    return executeWork(ticket)
}
```

### Naming

No single-letter variables except: `i`, `j`, `k` (loop indices), `t` (`*testing.T`), `b` (`*testing.B`), `r` (`*http.Request`), `w` (`http.ResponseWriter`), `ctx` (`context.Context`), `err` (errors), `ok`, `wg`. Enforced by `varnamelen` linter.

### File Size

**500 lines maximum** per file — both production and test files. Enforced by `scripts/check-filesize.sh` in the pre-commit hook. If a file approaches 500 lines, split it by responsibility.

### UK English

All user-facing strings, documentation, and comments use UK English. The `misspell` linter is configured with `locale: UK`.

### Design Principles

- **Interface-driven**: define small interfaces at the consumer site. Accept interfaces, return structs
- **Dependency injection**: all external dependencies passed via constructors. No global state, no `init()`
- **Error handling**: wrap errors with context using `fmt.Errorf("operation: %w", err)`. Never swallow errors
- **Context propagation**: pass `context.Context` as the first parameter to any function that does I/O

### Documentation

- Every exported function, type, and constant has a GoDoc comment starting with the name
- Package-level doc comment in a `doc.go` file for each package
- No inline comments unless explaining a non-obvious algorithm

## Git Hooks (Lefthook)

### Pre-commit (parallel)

| Hook | What it does |
|------|-------------|
| `gofumpt -l -w .` | Format all Go files (stricter than `gofmt`) |
| `golangci-lint run` | Lint with strict settings |
| `go vet ./...` | Static analysis |
| `scripts/check-filesize.sh` | No file over 500 lines |
| `scripts/check-secrets.sh` | Block commits containing API key patterns |

### Pre-push

| Hook | What it does |
|------|-------------|
| `scripts/check-coverage.sh 95` | All tests pass with race detector, 95% minimum coverage |

## Linter Configuration

`.golangci.yml` enables these linters:

`errcheck`, `govet`, `staticcheck`, `unused`, `gosimple`, `ineffassign`, `typecheck`, `gocritic`, `revive`, `gofumpt`, `misspell`, `unconvert`, `unparam`, `nakedret`, `nestif`, `varnamelen`, `exhaustive`, `forcetypeassert`, `goconst`, `nilerr`, `prealloc`

Key settings:
- `nestif.min-complexity: 1` — zero tolerance for nesting
- `misspell.locale: UK` — British English
- `varnamelen.min-name-length: 2` — no single-character variables (with exceptions)
- `exhaustive` — switch statements must handle all enum cases
- `forcetypeassert` — no unchecked type assertions
- Build tag `sqlite_fts5` required

## Task Commands

| Command | Description |
|---------|-------------|
| `task build` | Build both binaries |
| `task start` | Build and start |
| `task restart` | Rebuild, kill, start fresh |
| `task stop` | Kill running process |
| `task status` | Show agent statuses |
| `task test` | Run all tests with race detector |
| `task cover` | Generate coverage report |
| `task lint` | Run linter |
| `task fmt` | Format all Go files |
| `task clean` | Remove build artefacts |
| `task setup` | Install all development tools |

## Dependencies

Minimal and justified. No ORMs, no framework-heavy libraries:

| Dependency | Purpose |
|-----------|---------|
| `mattn/go-sqlite3` | SQLite driver (CGo) |
| `slack-go/slack` | Slack API with socket mode |
| `spf13/cobra` | CLI framework |
| `BurntSushi/toml` | TOML config parsing |
| `stretchr/testify` | Test assertions |
| `charmbracelet/bubbletea` | Terminal UI framework |
| `charmbracelet/lipgloss` | Terminal styling |
| `golang-jwt/jwt/v5` | JWT generation for GitHub App |

## Setup

```bash
git clone https://github.com/JR-G/squad0.git
cd squad0
./scripts/install.sh    # Installs Go tools, git hooks, Ollama model, builds binaries
```

The install script is idempotent — it skips anything already installed.
