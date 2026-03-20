# Development

## Tech Stack

- **Language**: Go 1.22+
- **Database**: SQLite via `mattn/go-sqlite3` (CGo) with FTS5
- **Slack**: `slack-go/slack`
- **CLI**: `spf13/cobra`
- **Config**: `BurntSushi/toml`
- **Testing**: `stretchr/testify`
- **Embeddings**: Ollama (`nomic-embed-text`) — local, free, offline
- **Build**: `-tags sqlite_fts5` required for all build and test commands

## Code Standards

### Control Flow
- **No nested ifs.** Guard clauses and early returns only. Enforced by `nestif` linter with complexity 1.
- Prefer `switch` over `if-else` chains.

### Naming
- No single-letter variables except: `i`, `j`, `k` (loops), `t` (testing), `ctx`, `err`, `ok`, `wg`.
- Enforced by `varnamelen` linter.

### File Size
- **500 lines max** per file (production and test). Enforced by pre-commit hook.

### Types
- Exhaustive switch statements. Enforced by `exhaustive` linter.
- No unchecked type assertions. Enforced by `forcetypeassert` linter.
- Pre-allocate slices when length is known.

### UK English
- All user-facing strings, documentation, and comments use UK English.
- The `misspell` linter is configured with `locale: UK`.

## Git Hooks (Lefthook)

### Pre-commit
- `gofumpt` — format all Go files
- `golangci-lint run` — lint with strict settings
- `go vet` — static analysis
- `scripts/check-filesize.sh` — no file over 500 lines
- `scripts/check-secrets.sh` — block commits containing API key patterns

### Pre-push
- `go test -race -tags sqlite_fts5 ./...` — all tests pass with race detector
- `scripts/check-coverage.sh 95` — 95% minimum test coverage

## Testing

- 95% coverage minimum, enforced on push
- Table-driven tests as default pattern
- `t.Parallel()` on all tests
- Test names: `TestFunctionName_Scenario_ExpectedBehaviour`
- In-memory SQLite (`:memory:`) for fast database tests
- `httptest` for HTTP API mocking (Slack, Ollama)
- Interface-driven design for all external dependencies

## Building

```bash
# Main binary
go build -tags sqlite_fts5 -o bin/squad0 ./cmd/squad0

# MCP memory server
go build -tags sqlite_fts5 -o bin/squad0-memory-mcp ./cmd/squad0-memory-mcp

# Run tests
go test -race -tags sqlite_fts5 ./...

# Run linter
golangci-lint run ./...

# Format
gofumpt -l -w .
```

## Dependencies

Minimal and justified. No ORMs, no framework-heavy libraries:

- `mattn/go-sqlite3` — SQLite driver (CGo)
- `slack-go/slack` — Slack API
- `spf13/cobra` — CLI framework
- `BurntSushi/toml` — config parsing
- `stretchr/testify` — test assertions
