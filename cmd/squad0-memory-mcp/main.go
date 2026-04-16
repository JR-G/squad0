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

func main() {
	dbPath := flag.String("db", "", "path to the agent's SQLite database (overridden by "+envDBPath+" env var)")
	flag.Parse()

	resolved := resolveDBPath(*dbPath, os.Getenv(envDBPath))
	if resolved == "" {
		fmt.Fprintf(os.Stderr, "error: set %s env var or pass --db\n", envDBPath)
		os.Exit(1)
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

	handler := mcp.NewMemoryHandler(graphStore, factStore, episodeStore, retriever)
	server := mcp.NewServer(handler)

	return server.Run(ctx)
}
