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
		if err := writeDump(store, dest, *force); err != nil {
			return err
		}
	default:
		if *force {
			if err := os.Remove(dest); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("remove existing destination %q: %w", dest, err)
			}
		}
		if err := store.BackupTo(dest); err != nil {
			return fmt.Errorf("backup to %q: %w", dest, err)
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

// writeDump writes a SQL dump to dest, truncating any existing file only
// when force is set (Run has already verified the collision rule).
func writeDump(store Backupper, dest string, force bool) error {
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}

	f, err := os.OpenFile(dest, flags, 0o600)
	if err != nil {
		return fmt.Errorf("create %q: %w", dest, err)
	}
	defer f.Close()

	if err := store.Dump(f); err != nil {
		return fmt.Errorf("dump to %q: %w", dest, err)
	}
	return f.Close()
}
