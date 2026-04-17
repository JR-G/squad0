package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/tui"
)

const ollamaHealthcheckTimeout = 5 * time.Second

// checkOllamaHealth performs a real embed call against the
// configured Ollama instance + model. Logs a TUI step on success or
// a warning on failure — non-fatal because squad0 runs fine with
// vector search degraded to keyword-only, but operators should know
// at startup rather than discovering it via "vector search skipped"
// log lines hours later.
func checkOllamaHealth(ctx context.Context, ollamaURL, model string, out io.Writer) {
	if ollamaURL == "" || model == "" {
		_, _ = fmt.Fprint(out, tui.StepWarn("Ollama not configured — vector search disabled"))
		return
	}

	probeCtx, cancel := context.WithTimeout(ctx, ollamaHealthcheckTimeout)
	defer cancel()

	embedder := memory.NewEmbedder(ollamaURL, model)
	if _, err := embedder.Embed(probeCtx, "ping"); err != nil {
		_, _ = fmt.Fprint(out, tui.StepWarn(fmt.Sprintf("Ollama unreachable (%s): vector search will be disabled — %v", model, err)))
		return
	}
	_, _ = fmt.Fprint(out, tui.StepDone(fmt.Sprintf("Ollama embedder ready (%s)", model)))
}
