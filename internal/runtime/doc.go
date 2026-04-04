// Package runtime provides a runtime-agnostic abstraction for agent
// session execution. Both Claude Code and Codex are first-class
// runtimes — configurable per agent, switchable at any time.
//
// The Runtime interface supports two modes: persistent sessions
// (tmux + hooks for Claude Code) and fresh processes (for Codex
// or Claude Code without persistence). The SessionBridge wraps
// the active runtime and handles transparent fallback on rate limits.
package runtime
