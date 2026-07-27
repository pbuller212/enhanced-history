package db

import (
	"bytes"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"enhanced-history/internal/query"
	"enhanced-history/internal/record"

	_ "modernc.org/sqlite"
)

func openTemp(t *testing.T) *DB {
	t.Helper()
	d := t.TempDir()
	db, err := Open(filepath.Join(d, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSchemaCreated(t *testing.T) {
	db := openTemp(t)

	tables := []string{"history"}
	for _, name := range tables {
		var n int
		err := db.sql.QueryRow(
			`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, name,
		).Scan(&n)
		if err != nil {
			t.Fatalf("query sqlite_master: %v", err)
		}
		if n != 1 {
			t.Errorf("table %q not found", name)
		}
	}

	indexes := []string{"idx_ts", "idx_dir", "idx_session"}
	for _, name := range indexes {
		var n int
		err := db.sql.QueryRow(
			`SELECT count(*) FROM sqlite_master WHERE type='index' AND name=?`, name,
		).Scan(&n)
		if err != nil {
			t.Fatalf("query sqlite_master: %v", err)
		}
		if n != 1 {
			t.Errorf("index %q not found", name)
		}
	}
}

func TestInsertAndQuery(t *testing.T) {
	db := openTemp(t)

	r := record.Record{Ts: 1000, Dir: "/home/user", Session: "sess-1", Cmd: "ls -la"}
	if err := db.Insert(r); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	rows, err := db.Query(query.Filter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	got := rows[0]
	if got.Ts != r.Ts || got.Dir != r.Dir || got.Session != r.Session || got.Cmd != r.Cmd {
		t.Errorf("round-trip mismatch: got %+v", got)
	}
	if got.ID == 0 {
		t.Error("ID should be non-zero after insert")
	}
}

func TestFilterTsRange(t *testing.T) {
	db := openTemp(t)

	for _, ts := range []int64{100, 200, 300} {
		if err := db.Insert(record.Record{Ts: ts, Dir: "/", Session: "s", Cmd: "cmd"}); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := db.Query(query.Filter{TsFrom: 150, TsTo: 250})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Ts != 200 {
		t.Errorf("expected 1 row with ts=200, got %+v", rows)
	}
}

func TestFilterDir(t *testing.T) {
	db := openTemp(t)

	dirs := []string{"/home/user/projects", "/home/user/other", "/var/log"}
	for _, d := range dirs {
		if err := db.Insert(record.Record{Ts: 1, Dir: d, Session: "s", Cmd: "cmd"}); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := db.Query(query.Filter{Dir: "/home/user"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Errorf("expected 2 rows matching /home/user prefix, got %d", len(rows))
	}
}

func TestFilterDirExact(t *testing.T) {
	db := openTemp(t)

	dirs := []string{"/home/user", "/home/user/projects", "/home/userx"}
	for _, d := range dirs {
		if err := db.Insert(record.Record{Ts: 1, Dir: d, Session: "s", Cmd: "cmd"}); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := db.Query(query.Filter{Dir: "/home/user", DirExact: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Dir != "/home/user" {
		t.Errorf("expected 1 row matching /home/user exactly, got %+v", rows)
	}
}

func TestFilterSession(t *testing.T) {
	db := openTemp(t)

	for _, sess := range []string{"aaa", "bbb", "aaa"} {
		if err := db.Insert(record.Record{Ts: 1, Dir: "/", Session: sess, Cmd: "cmd"}); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := db.Query(query.Filter{Session: "aaa"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Errorf("expected 2 rows for session=aaa, got %d", len(rows))
	}
}

func TestFilterCmdSubstr(t *testing.T) {
	db := openTemp(t)

	cmds := []string{"git status", "git commit -m 'msg'", "ls -la"}
	for _, c := range cmds {
		if err := db.Insert(record.Record{Ts: 1, Dir: "/", Session: "s", Cmd: c}); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := db.Query(query.Filter{CmdSubstr: "git"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Errorf("expected 2 rows matching 'git', got %d", len(rows))
	}
}

func TestFilterLikeWildcardsEscaped(t *testing.T) {
	db := openTemp(t)

	dirs := []string{"/home/user_projects", "/home/user/projects", "/home/user%all"}
	for _, d := range dirs {
		if err := db.Insert(record.Record{Ts: 1, Dir: d, Session: "s", Cmd: "cmd"}); err != nil {
			t.Fatal(err)
		}
	}

	// "_" must be matched literally, not as a single-char wildcard.
	rows, err := db.Query(query.Filter{Dir: "/home/user_"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Dir != "/home/user_projects" {
		t.Errorf("expected exactly /home/user_projects, got %+v", rows)
	}

	// "%" must be matched literally, not as a multi-char wildcard.
	rows, err = db.Query(query.Filter{Dir: "/home/user%"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Dir != "/home/user%all" {
		t.Errorf("expected exactly /home/user%%all, got %+v", rows)
	}
}

func TestBackupToCreatesOpenableCopyWithMatchingData(t *testing.T) {
	db := openTemp(t)

	records := []record.Record{
		{Ts: 100, Dir: "/home/user", Session: "sess-1", Cmd: "ls -la"},
		{Ts: 200, Dir: "/tmp", Session: "sess-2", Cmd: "echo 'hi'"},
	}
	for _, r := range records {
		if err := db.Insert(r); err != nil {
			t.Fatal(err)
		}
	}

	dest := filepath.Join(t.TempDir(), "backup.db")
	if err := db.BackupTo(dest); err != nil {
		t.Fatalf("BackupTo: %v", err)
	}

	backup, err := Open(dest)
	if err != nil {
		t.Fatalf("Open backup: %v", err)
	}
	defer backup.Close()

	rows, err := backup.Query(query.Filter{})
	if err != nil {
		t.Fatalf("Query backup: %v", err)
	}
	if len(rows) != len(records) {
		t.Fatalf("expected %d rows in backup, got %d", len(records), len(rows))
	}
	for i, r := range rows {
		if r.Ts != records[i].Ts || r.Dir != records[i].Dir || r.Session != records[i].Session || r.Cmd != records[i].Cmd {
			t.Errorf("row %d mismatch: got %+v, want %+v", i, r, records[i])
		}
	}
}

func TestBackupToRefusesExistingDestination(t *testing.T) {
	db := openTemp(t)
	if err := db.Insert(record.Record{Ts: 1, Dir: "/", Session: "s", Cmd: "cmd"}); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "backup.db")
	if err := db.BackupTo(dest); err != nil {
		t.Fatalf("first BackupTo: %v", err)
	}

	if err := db.BackupTo(dest); err == nil {
		t.Error("expected second BackupTo to the same path to fail, got nil error")
	}
}

func TestDumpRoundTrips(t *testing.T) {
	db := openTemp(t)

	records := []record.Record{
		{Ts: 100, Dir: "/home/user", Session: "sess-1", Cmd: "ls -la"},
		{Ts: 200, Dir: "/tmp", Session: "sess-2", Cmd: `echo "it's a test"`},
	}
	for _, r := range records {
		if err := db.Insert(r); err != nil {
			t.Fatal(err)
		}
	}

	var buf bytes.Buffer
	if err := db.Dump(&buf); err != nil {
		t.Fatalf("Dump: %v", err)
	}

	dumpSQL := buf.String()
	if !strings.Contains(dumpSQL, "CREATE TABLE") {
		t.Error("dump missing CREATE TABLE statement")
	}
	if !strings.Contains(dumpSQL, "INSERT INTO history") {
		t.Error("dump missing INSERT statements")
	}

	// Replay the dump against a fresh, empty database and verify the data matches.
	replayPath := filepath.Join(t.TempDir(), "replay.db")
	replaySQL, err := sql.Open("sqlite", replayPath)
	if err != nil {
		t.Fatalf("open replay db: %v", err)
	}
	defer replaySQL.Close()

	for _, stmt := range strings.Split(dumpSQL, ";\n") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := replaySQL.Exec(stmt); err != nil {
			t.Fatalf("replay statement %q: %v", stmt, err)
		}
	}

	replay, err := Open(replayPath)
	if err != nil {
		t.Fatalf("Open replay: %v", err)
	}
	defer replay.Close()

	rows, err := replay.Query(query.Filter{})
	if err != nil {
		t.Fatalf("Query replay: %v", err)
	}
	if len(rows) != len(records) {
		t.Fatalf("expected %d rows after replay, got %d", len(records), len(rows))
	}
	for i, r := range rows {
		if r.Ts != records[i].Ts || r.Dir != records[i].Dir || r.Session != records[i].Session || r.Cmd != records[i].Cmd {
			t.Errorf("row %d mismatch: got %+v, want %+v", i, r, records[i])
		}
	}
}
