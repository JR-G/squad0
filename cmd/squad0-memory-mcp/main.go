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

func main() {
	dbPath := flag.String("db", "", "path to the agent's SQLite database")
	flag.Parse()

	if *dbPath == "" {
		fmt.Fprintln(os.Stderr, "error: --db flag is required")
		os.Exit(1)
	}

	if err := run(*dbPath); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
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
