package db

import (
	"path/filepath"
	"testing"

	"enhanced-history/internal/query"
	"enhanced-history/internal/record"
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
