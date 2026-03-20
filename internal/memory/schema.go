package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	_ "github.com/mattn/go-sqlite3" // SQLite driver registration.
)

// DB wraps a *sql.DB connection with WAL mode enabled and the schema
// initialised. It is safe for concurrent use by multiple goroutines.
type DB struct {
	db *sql.DB
}

// Open creates or opens a SQLite database at the given path, enables WAL
// mode, and applies any pending schema migrations. The caller must call
// Close when finished.
func Open(ctx context.Context, dbPath string) (*DB, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_foreign_keys=ON&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if err := db.PingContext(ctx); err != nil {
		closeDB(db)
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	if err := ensureVersionTable(ctx, db); err != nil {
		closeDB(db)
		return nil, fmt.Errorf("ensuring version table: %w", err)
	}

	current, err := currentVersion(ctx, db)
	if err != nil {
		closeDB(db)
		return nil, fmt.Errorf("reading schema version: %w", err)
	}

	if err := applyMigrations(ctx, db, current); err != nil {
		closeDB(db)
		return nil, fmt.Errorf("applying migrations: %w", err)
	}

	return &DB{db: db}, nil
}

// Close closes the underlying database connection.
func (memDB *DB) Close() error {
	return memDB.db.Close()
}

// RawDB returns the underlying *sql.DB for use by stores that need
// direct access.
func (memDB *DB) RawDB() *sql.DB {
	return memDB.db
}

type migration struct {
	version     int
	description string
	apply       func(tx *sql.Tx) error
}

var migrations = []migration{
	{
		version:     1,
		description: "initial schema",
		apply:       applyInitialSchema,
	},
}

func closeDB(db *sql.DB) {
	_ = db.Close()
}

func rollback(tx *sql.Tx) {
	_ = tx.Rollback()
}

func ensureVersionTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`)
	return err
}

func currentVersion(ctx context.Context, db *sql.DB) (int, error) {
	var version int
	err := db.QueryRowContext(ctx, `SELECT version FROM schema_version LIMIT 1`).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return version, err
}

func applyMigrations(ctx context.Context, db *sql.DB, from int) error {
	for _, mig := range migrations {
		if mig.version <= from {
			continue
		}

		if err := applyOneMigration(ctx, db, mig, from); err != nil {
			return err
		}

		from = mig.version
	}

	return nil
}

func applyOneMigration(ctx context.Context, db *sql.DB, mig migration, from int) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning migration %d (%s): %w", mig.version, mig.description, err)
	}
	defer rollback(tx)

	if err := mig.apply(tx); err != nil {
		return fmt.Errorf("applying migration %d (%s): %w", mig.version, mig.description, err)
	}

	if err := upsertVersion(ctx, tx, mig.version, from); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing migration %d (%s): %w", mig.version, mig.description, err)
	}

	return nil
}

func upsertVersion(ctx context.Context, tx *sql.Tx, version, from int) error {
	var err error
	switch from {
	case 0:
		_, err = tx.ExecContext(ctx, `INSERT INTO schema_version (version) VALUES (?)`, version)
	default:
		_, err = tx.ExecContext(ctx, `UPDATE schema_version SET version = ?`, version)
	}

	if err != nil {
		return fmt.Errorf("updating schema version to %d: %w", version, err)
	}

	return nil
}

func applyInitialSchema(tx *sql.Tx) error {
	statements := []string{
		`CREATE TABLE entities (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			entity_type TEXT NOT NULL,
			name TEXT NOT NULL,
			summary TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE relationships (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source_id INTEGER REFERENCES entities(id),
			target_id INTEGER REFERENCES entities(id),
			relation_type TEXT NOT NULL,
			description TEXT,
			valid_from TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			valid_until TIMESTAMP,
			confidence REAL DEFAULT 0.5
		)`,
		`CREATE TABLE facts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			entity_id INTEGER REFERENCES entities(id),
			content TEXT NOT NULL,
			fact_type TEXT NOT NULL,
			confidence REAL DEFAULT 0.5,
			confirmations INTEGER DEFAULT 1,
			source_episode_id INTEGER REFERENCES episodes(id),
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			invalidated_at TIMESTAMP
		)`,
		`CREATE TABLE episodes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			agent TEXT NOT NULL,
			ticket TEXT,
			summary TEXT NOT NULL,
			embedding BLOB,
			outcome TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE beliefs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			content TEXT NOT NULL,
			confidence REAL DEFAULT 0.5,
			confirmations INTEGER DEFAULT 1,
			contradictions INTEGER DEFAULT 0,
			last_confirmed_at TIMESTAMP,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE VIRTUAL TABLE facts_fts USING fts5(content, fact_type, tokenize='porter')`,
		`CREATE VIRTUAL TABLE episodes_fts USING fts5(summary, ticket, tokenize='porter')`,
		`CREATE VIRTUAL TABLE beliefs_fts USING fts5(content, tokenize='porter')`,
	}

	for _, stmt := range statements {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("executing %q: %w", stmt[:40], err)
		}
	}

	return nil
}
