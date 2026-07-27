package backup

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeStore is a test double for Backupper that records calls instead of
// touching a real database.
type fakeStore struct {
	backupToCalls []string
	backupToErr   error
	dumpErr       error
	dumpContent   string
}

func (f *fakeStore) BackupTo(destPath string) error {
	f.backupToCalls = append(f.backupToCalls, destPath)
	if f.backupToErr != nil {
		return f.backupToErr
	}
	// Simulate VACUUM INTO by actually creating the file, since real SQLite
	// would refuse to run against a non-existent path's parent otherwise and
	// callers of Run check for file existence.
	return os.WriteFile(destPath, []byte("fake-db-contents"), 0o600)
}

func (f *fakeStore) Dump(w io.Writer) error {
	if f.dumpErr != nil {
		return f.dumpErr
	}
	content := f.dumpContent
	if content == "" {
		content = "CREATE TABLE history (...);\n"
	}
	_, err := w.Write([]byte(content))
	return err
}

func TestRunDefaultFilename(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USER", "alice")

	store := &fakeStore{}
	var out bytes.Buffer
	if err := Run(nil, store, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(store.backupToCalls) != 1 {
		t.Fatalf("expected 1 BackupTo call, got %d", len(store.backupToCalls))
	}
	dest := store.backupToCalls[0]

	if !strings.HasPrefix(dest, home) {
		t.Errorf("destination %q not under home %q", dest, home)
	}
	base := filepath.Base(dest)
	if !strings.HasPrefix(base, "alice_") || !strings.HasSuffix(base, ".db") {
		t.Errorf("unexpected default filename %q", base)
	}

	if !strings.Contains(out.String(), "Backup written to "+dest) {
		t.Errorf("unexpected stdout: %q", out.String())
	}
}

func TestRunExplicitDestination(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "mybackup.db")

	store := &fakeStore{}
	var out bytes.Buffer
	if err := Run([]string{dest}, store, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(store.backupToCalls) != 1 || store.backupToCalls[0] != dest {
		t.Fatalf("expected BackupTo(%q), got %+v", dest, store.backupToCalls)
	}
	if !strings.Contains(out.String(), "Backup written to "+dest) {
		t.Errorf("unexpected stdout: %q", out.String())
	}
}

func TestRunRelativeDestinationResolvedToAbsolute(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(cwd) })

	store := &fakeStore{}
	var out bytes.Buffer
	if err := Run([]string{"rel.db"}, store, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}

	dest := store.backupToCalls[0]
	if !filepath.IsAbs(dest) {
		t.Errorf("expected absolute path, got %q", dest)
	}
}

func TestRunFormatSQLProducesDump(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "backup.sql")

	store := &fakeStore{dumpContent: "CREATE TABLE history (id INTEGER);\nINSERT INTO history VALUES (1);\n"}
	var out bytes.Buffer
	if err := Run([]string{"--format", "sql", dest}, store, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(store.backupToCalls) != 0 {
		t.Errorf("expected BackupTo not to be called for sql format, got %+v", store.backupToCalls)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dump file: %v", err)
	}
	if !strings.Contains(string(got), "CREATE TABLE") || !strings.Contains(string(got), "INSERT INTO") {
		t.Errorf("unexpected dump content: %q", got)
	}
}

func TestRunDefaultFilenameFormatSQLUsesSQLExtension(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USER", "alice")

	store := &fakeStore{}
	var out bytes.Buffer
	if err := Run([]string{"--format", "sql"}, store, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}

	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), ".sql") {
		t.Errorf("expected exactly one .sql file in home, got %+v", entries)
	}
}

func TestRunCollisionWithoutForceErrors(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "backup.db")
	if err := os.WriteFile(dest, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}

	store := &fakeStore{}
	var out bytes.Buffer
	err := Run([]string{dest}, store, &out)
	if err == nil {
		t.Fatal("expected error on collision without --force, got nil")
	}
	if len(store.backupToCalls) != 0 {
		t.Errorf("BackupTo should not have been called, got %+v", store.backupToCalls)
	}
	if out.Len() != 0 {
		t.Errorf("expected no stdout output on error, got %q", out.String())
	}

	// Original file must be untouched.
	content, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "existing" {
		t.Errorf("existing file was modified: %q", content)
	}
}

func TestRunCollisionWithForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "backup.db")
	if err := os.WriteFile(dest, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}

	store := &fakeStore{}
	var out bytes.Buffer
	if err := Run([]string{"--force", dest}, store, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(store.backupToCalls) != 1 || store.backupToCalls[0] != dest {
		t.Fatalf("expected BackupTo(%q), got %+v", dest, store.backupToCalls)
	}
	content, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "fake-db-contents" {
		t.Errorf("expected file to be overwritten, got %q", content)
	}
}

func TestRunCollisionSQLWithForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "backup.sql")
	if err := os.WriteFile(dest, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}

	store := &fakeStore{dumpContent: "CREATE TABLE history (id INTEGER);\n"}
	var out bytes.Buffer
	if err := Run([]string{"--force", "--format", "sql", dest}, store, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}

	content, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "CREATE TABLE") {
		t.Errorf("expected file to be overwritten with dump, got %q", content)
	}
}

func TestRunInvalidFormat(t *testing.T) {
	store := &fakeStore{}
	var out bytes.Buffer
	err := Run([]string{"--format", "xml"}, store, &out)
	if err == nil {
		t.Fatal("expected error for invalid --format, got nil")
	}
}

func TestRunTooManyArgs(t *testing.T) {
	store := &fakeStore{}
	var out bytes.Buffer
	err := Run([]string{"a.db", "b.db"}, store, &out)
	if err == nil {
		t.Fatal("expected error for too many positional args, got nil")
	}
}

func TestRunHelp(t *testing.T) {
	store := &fakeStore{}
	var out bytes.Buffer
	err := Run([]string{"--help"}, store, &out)
	if !errors.Is(err, ErrHelp) {
		t.Fatalf("expected ErrHelp, got %v", err)
	}
	if !strings.Contains(out.String(), "Usage: hist backup") {
		t.Errorf("unexpected help output: %q", out.String())
	}
	if len(store.backupToCalls) != 0 {
		t.Error("BackupTo should not be called when --help is passed")
	}
}

func TestRunBackupToErrorPropagates(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "backup.db")

	store := &fakeStore{backupToErr: errors.New("disk full")}
	var out bytes.Buffer
	err := Run([]string{dest}, store, &out)
	if err == nil {
		t.Fatal("expected error to propagate from BackupTo, got nil")
	}
	if out.Len() != 0 {
		t.Errorf("expected no stdout output on error, got %q", out.String())
	}
}
