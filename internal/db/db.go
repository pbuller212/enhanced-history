package db

import (
	"database/sql"
	"fmt"
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
