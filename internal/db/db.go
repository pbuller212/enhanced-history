package db

import (
	"database/sql"
	"fmt"
	"io"
	"strings"

	"enhanced-history/internal/query"
	"enhanced-history/internal/record"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS history (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    ts      INTEGER NOT NULL,
    dir     TEXT    NOT NULL,
    session TEXT    NOT NULL,
    cmd     TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ts      ON history(ts);
CREATE INDEX IF NOT EXISTS idx_dir     ON history(dir);
CREATE INDEX IF NOT EXISTS idx_session ON history(session);
`

// DB wraps a sql.DB and exposes history-specific operations.
type DB struct {
	sql *sql.DB
}

// Open opens (or creates) the SQLite database at path, enables WAL mode, and
// applies the schema. The caller owns the returned DB and must call Close.
func Open(path string) (*DB, error) {
	sqldb, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if _, err = sqldb.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	if _, err = sqldb.Exec(schema); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	return &DB{sql: sqldb}, nil
}

// Close closes the underlying database connection.
func (d *DB) Close() error {
	return d.sql.Close()
}

// Insert writes a single history record. r.ID is ignored; the DB assigns it.
func (d *DB) Insert(r record.Record) error {
	_, err := d.sql.Exec(
		`INSERT INTO history (ts, dir, session, cmd) VALUES (?, ?, ?, ?)`,
		r.Ts, r.Dir, r.Session, r.Cmd,
	)
	if err != nil {
		return fmt.Errorf("insert record: %w", err)
	}
	return nil
}

// escapeLike escapes SQLite LIKE special characters so the value is treated literally.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "%", `\%`)
	s = strings.ReplaceAll(s, "_", `\_`)
	return s
}

// Query returns history rows matching all non-zero fields of f, ordered by ts ASC.
func (d *DB) Query(f query.Filter) ([]record.Record, error) {
	q := `SELECT id, ts, dir, session, cmd FROM history WHERE 1=1`
	args := []any{}

	if f.TsFrom != 0 {
		q += ` AND ts >= ?`
		args = append(args, f.TsFrom)
	}
	if f.TsTo != 0 {
		q += ` AND ts <= ?`
		args = append(args, f.TsTo)
	}
	if f.Dir != "" {
		if f.DirExact {
			q += ` AND dir = ?`
			args = append(args, f.Dir)
		} else {
			q += ` AND dir LIKE ? || '%' ESCAPE '\'`
			args = append(args, escapeLike(f.Dir))
		}
	}
	if f.Session != "" {
		q += ` AND session = ?`
		args = append(args, f.Session)
	}
	if f.CmdSubstr != "" {
		q += ` AND cmd LIKE '%' || ? || '%' ESCAPE '\'`
		args = append(args, escapeLike(f.CmdSubstr))
	}

	q += ` ORDER BY ts ASC`

	rows, err := d.sql.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("query history: %w", err)
	}
	defer rows.Close()

	var results []record.Record
	for rows.Next() {
		var r record.Record
		if err := rows.Scan(&r.ID, &r.Ts, &r.Dir, &r.Session, &r.Cmd); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}
	return results, nil
}

// BackupTo writes an atomic, consistent binary copy of the database to
// destPath using SQLite's VACUUM INTO. destPath must not already exist;
// VACUUM INTO refuses to overwrite an existing file.
func (d *DB) BackupTo(destPath string) error {
	if _, err := d.sql.Exec(`VACUUM INTO ?`, destPath); err != nil {
		return fmt.Errorf("vacuum into %q: %w", destPath, err)
	}
	return nil
}

// sqlQuote returns s as a single-quoted SQL string literal with embedded
// quotes doubled, suitable for embedding in generated SQL text.
func sqlQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// Dump writes a plain-text SQL rendering of the database to w: the schema
// (CREATE TABLE / CREATE INDEX statements from sqlite_master) followed by an
// INSERT statement per row of the history table. The output can be replayed
// against sqlite3 (or Open + Exec) to reconstruct the database.
func (d *DB) Dump(w io.Writer) error {
	schemaRows, err := d.sql.Query(
		`SELECT sql FROM sqlite_master WHERE type IN ('table', 'index') AND sql IS NOT NULL AND name NOT LIKE 'sqlite_%' ORDER BY type DESC, name`,
	)
	if err != nil {
		return fmt.Errorf("query schema: %w", err)
	}
	defer schemaRows.Close()

	for schemaRows.Next() {
		var stmt string
		if err := schemaRows.Scan(&stmt); err != nil {
			return fmt.Errorf("scan schema row: %w", err)
		}
		if _, err := fmt.Fprintf(w, "%s;\n", stmt); err != nil {
			return fmt.Errorf("write schema statement: %w", err)
		}
	}
	if err := schemaRows.Err(); err != nil {
		return fmt.Errorf("iterate schema rows: %w", err)
	}

	dataRows, err := d.sql.Query(`SELECT id, ts, dir, session, cmd FROM history ORDER BY id ASC`)
	if err != nil {
		return fmt.Errorf("query history: %w", err)
	}
	defer dataRows.Close()

	for dataRows.Next() {
		var r record.Record
		if err := dataRows.Scan(&r.ID, &r.Ts, &r.Dir, &r.Session, &r.Cmd); err != nil {
			return fmt.Errorf("scan history row: %w", err)
		}
		_, err := fmt.Fprintf(w, "INSERT INTO history (id, ts, dir, session, cmd) VALUES (%d, %d, %s, %s, %s);\n",
			r.ID, r.Ts, sqlQuote(r.Dir), sqlQuote(r.Session), sqlQuote(r.Cmd))
		if err != nil {
			return fmt.Errorf("write insert statement: %w", err)
		}
	}
	if err := dataRows.Err(); err != nil {
		return fmt.Errorf("iterate history rows: %w", err)
	}

	return nil
}
