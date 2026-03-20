// Package memory provides a hybrid knowledge graph and vector search system
// backed by SQLite. Each agent maintains a personal database of entities,
// facts, beliefs, and episodes. A shared project database holds cross-cutting
// knowledge. All databases use WAL mode for concurrent access.
package memory
