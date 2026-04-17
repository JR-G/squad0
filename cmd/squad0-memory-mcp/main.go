package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/JR-G/squad0/internal/mcp"
	"github.com/JR-G/squad0/internal/memory"
)

// envDBPath is the env var the orchestrator sets per agent so a single
// user-scope MCP registration can serve every agent with its own DB.
// Env wins over --db because user-scope registration uses fixed argv;
// the orchestrator can only vary the env on each spawn.
const envDBPath = "SQUAD0_MEMORY_DB"

const envSessionID = "SQUAD0_SESSION_ID"

// fallbackDBPath is what we open when neither --db nor SQUAD0_MEMORY_DB
// is provided. It keeps the server connectable in interactive
// `claude` sessions where the env var isn't set — without it, the
// binary exits and `claude mcp list` permanently shows
// "squad0-memory ✘ failed". The in-memory DB has no persisted data,
// so memory tools work but return nothing useful, which is the
// honest behaviour when no agent context is provided.
const fallbackDBPath = ":memory:"

func main() {
	dbPath := flag.String("db", "", "path to the agent's SQLite database (overridden by "+envDBPath+" env var)")
	flag.Parse()

	resolved := resolveDBPath(*dbPath, os.Getenv(envDBPath))
	if resolved == "" {
		fmt.Fprintf(os.Stderr, "warn: %s not set and --db not passed — running with an empty in-memory DB\n", envDBPath)
		resolved = fallbackDBPath
	}

	if err := run(resolved); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// resolveDBPath returns whichever of envValue or flagValue is set,
// preferring envValue. Exposed for testing.
func resolveDBPath(flagValue, envValue string) string {
	if envValue != "" {
		return envValue
	}
	return flagValue
}

func run(dbPath string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	memDB, err := memory.Open(ctx, dbPath)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer func() { _ = memDB.Close() }()

	graphStore := memory.NewGraphStore(memDB)
	factStore := memory.NewFactStore(memDB)
	episodeStore := memory.NewEpisodeStore(memDB)
	ftsStore := memory.NewFTSStore(memDB)
	embedder := memory.NewEmbedder("http://localhost:11434", "nomic-embed-text")
	hybridSearcher := memory.NewHybridSearcher(ftsStore, episodeStore, embedder, 0.5, 0.5)
	retriever := memory.NewRetriever(graphStore, factStore, episodeStore, hybridSearcher, ftsStore, 2, 20)

	workingStore := memory.NewWorkingStore(memDB)
	sessionID := os.Getenv(envSessionID)

	handler := mcp.NewMemoryHandler(graphStore, factStore, episodeStore, retriever).
		WithWorkingMemory(workingStore, sessionID)
	server := mcp.NewServer(handler)

	return server.Run(ctx)
}
