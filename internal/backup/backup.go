package backup

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// ErrHelp is returned by Run when --help or -h is passed. The caller should
// treat this as a clean exit (code 0), not an error.
var ErrHelp = errors.New("help requested")

// usageText is the full help block printed to out on --help/-h.
// New flags added to Run must have a corresponding line here.
const usageText = `Usage: hist backup [destination] [flags]

Back up the history database to destination, or to a default path under
$HOME if destination is omitted.

Flags:
  --format string   backup format: "db" (binary copy) or "sql" (plain-text dump) (default "db")
  --force           overwrite destination if it already exists

`

// Backupper is satisfied by db.DB; defined here to avoid an import cycle.
type Backupper interface {
	BackupTo(destPath string) error
	Dump(w io.Writer) error
}

// Run parses args, resolves the backup destination, and writes a backup via
// store. Errors are returned to the caller; nothing is written to out when an
// error occurs. Returns ErrHelp (exit 0) when --help/-h is passed.
func Run(args []string, store Backupper, out io.Writer) error {
	// Intercept --help/-h before flag parsing so output goes to out, not stderr.
	for _, a := range args {
		if a == "--help" || a == "-h" {
			fmt.Fprint(out, usageText)
			return ErrHelp
		}
	}

	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	format := fs.String("format", "db", `backup format: "db" or "sql"`)
	force := fs.Bool("force", false, "overwrite destination if it already exists")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}

	if *format != "db" && *format != "sql" {
		return fmt.Errorf("invalid --format %q: must be \"db\" or \"sql\"", *format)
	}

	positional := fs.Args()
	if len(positional) > 1 {
		return fmt.Errorf("too many arguments: expected at most one destination path")
	}

	dest := ""
	if len(positional) == 1 {
		dest = positional[0]
	}

	dest, err := resolveDestination(dest, *format)
	if err != nil {
		return err
	}

	if !*force {
		if _, err := os.Stat(dest); err == nil {
			return fmt.Errorf("destination %q already exists (use --force to overwrite)", dest)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("check destination %q: %w", dest, err)
		}
	}

	switch *format {
	case "sql":
		if err := writeDumpAtomic(store, dest); err != nil {
			return err
		}
	default:
		if err := backupToAtomic(store, dest); err != nil {
			return err
		}
	}

	fmt.Fprintf(out, "Backup written to %s\n", dest)
	return nil
}

// resolveDestination returns dest as an absolute path if non-empty, or
// computes the default $HOME/<USER>_<HOST>_<DATESTAMP>.<ext> path for format
// when dest is empty.
func resolveDestination(dest, format string) (string, error) {
	if dest != "" {
		abs, err := filepath.Abs(dest)
		if err != nil {
			return "", fmt.Errorf("resolve destination path: %w", err)
		}
		return abs, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}

	user := os.Getenv("USER")
	if user == "" {
		user = "user"
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "host"
	}
	datestamp := time.Now().Format("20060102")

	ext := "db"
	if format == "sql" {
		ext = "sql"
	}

	filename := fmt.Sprintf("%s_%s_%s.%s", user, host, datestamp, ext)
	return filepath.Join(home, filename), nil
}

// writeDumpAtomic writes a SQL dump to a temp file next to dest and renames
// it into place only once the dump has been written and closed successfully,
// so a failed or partial dump never touches (or destroys) an existing dest.
func writeDumpAtomic(store Backupper, dest string) error {
	tmp, err := tempPathFor(dest)
	if err != nil {
		return err
	}
	defer os.Remove(tmp) // no-op once the rename below has succeeded

	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open temp file %q: %w", tmp, err)
	}
	if err := store.Dump(f); err != nil {
		f.Close()
		return fmt.Errorf("dump to %q: %w", dest, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close temp file %q: %w", tmp, err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		return fmt.Errorf("rename %q to %q: %w", tmp, dest, err)
	}
	return nil
}

// backupToAtomic runs store.BackupTo against a temp path next to dest and
// renames the result into place only on success, so a failed backup never
// removes or truncates an existing dest. store.BackupTo (SQLite VACUUM INTO)
// refuses to write to a path that already exists, so tempPathFor's freshly
// created (and empty) temp file is removed first to make way for it.
func backupToAtomic(store Backupper, dest string) error {
	tmp, err := tempPathFor(dest)
	if err != nil {
		return err
	}
	defer os.Remove(tmp) // no-op once the rename below has succeeded

	if err := os.Remove(tmp); err != nil {
		return fmt.Errorf("remove placeholder temp file %q: %w", tmp, err)
	}
	if err := store.BackupTo(tmp); err != nil {
		return fmt.Errorf("backup to %q: %w", dest, err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		return fmt.Errorf("rename %q to %q: %w", tmp, dest, err)
	}
	return nil
}

// tempPathFor creates and returns an empty temp file in the same directory
// as dest, so a later rename onto dest is same-filesystem (and therefore
// atomic). The caller is responsible for removing it (directly, or via a
// successful rename).
func tempPathFor(dest string) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(dest), filepath.Base(dest)+".tmp-*")
	if err != nil {
		return "", fmt.Errorf("create temp file for %q: %w", dest, err)
	}
	tmp := f.Name()
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("close temp file %q: %w", tmp, err)
	}
	return tmp, nil
}
