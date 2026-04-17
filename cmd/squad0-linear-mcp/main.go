package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/JR-G/squad0/internal/integrations/linear"
	"github.com/JR-G/squad0/internal/mcp"
)

// envAPIKey is read at startup so the server gets the Keychain-
// backed Linear key without ever touching disk. Set by squad0 when
// it registers this binary user-scope via `claude mcp add --env`.
const envAPIKey = "LINEAR_API_KEY"

func main() {
	os.Exit(realMain(os.Getenv(envAPIKey), os.Stderr))
}

// realMain is the testable entry point. Returns the exit code and
// writes any fatal error to errOut.
func realMain(apiKey string, errOut io.Writer) int {
	if apiKey == "" {
		_, _ = fmt.Fprintf(errOut, "error: %s not set — squad0-linear-mcp cannot start without a Linear API key\n", envAPIKey)
		return 1
	}

	if err := run(apiKey); err != nil {
		_, _ = fmt.Fprintf(errOut, "error: %v\n", err)
		return 1
	}
	return 0
}

func run(apiKey string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	client := linear.NewClient(apiKey)
	handler := mcp.NewLinearHandler(client)
	server := mcp.NewServer(handler)

	return server.Run(ctx)
}
