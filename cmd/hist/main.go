package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"enhanced-history/internal/backup"
	"enhanced-history/internal/db"
	"enhanced-history/internal/query"
	"enhanced-history/internal/record"
	"enhanced-history/internal/tui"
)

// Version is injected at build time via -ldflags "-X enhanced-history/cmd/hist/main.Version=vX.Y.Z".
var Version = "dev"

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "--version" {
		fmt.Println(Version)
		return
	}

	// Intercept --help/-h before opening the DB (same pattern as --version).
	if len(os.Args) >= 2 && (os.Args[1] == "--help" || os.Args[1] == "-h") {
		if err := runQuery(os.Args[1:]); err != nil && !errors.Is(err, query.ErrHelp) {
			fmt.Fprintf(os.Stderr, "hist: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// With no arguments, or any arguments that don't match a sub-command,
	// default to the query path.
	if len(os.Args) >= 2 && os.Args[1] == "record" {
		if err := runRecord(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "hist record: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if len(os.Args) >= 2 && os.Args[1] == "backup" {
		if err := runBackup(os.Args[2:]); err != nil && !errors.Is(err, backup.ErrHelp) {
			fmt.Fprintf(os.Stderr, "hist backup: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if len(os.Args) >= 2 && os.Args[1] == "tui" {
		if err := runTUI(); err != nil {
			fmt.Fprintf(os.Stderr, "hist tui: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// All other invocations (bare `hist`, or `hist --since ...` etc.) query.
	args := os.Args[1:]
	if len(os.Args) >= 2 && os.Args[1] == "query" {
		// Allow explicit `hist query` as an alias for discoverability.
		args = os.Args[2:]
	}
	if err := runQuery(args); err != nil && !errors.Is(err, query.ErrHelp) {
		fmt.Fprintf(os.Stderr, "hist: %v\n", err)
		os.Exit(1)
	}
}

func openDB() (*db.DB, error) {
	dbPath := os.Getenv("ENHANCED_HISTORY_DB")
	if dbPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home dir: %w", err)
		}
		dbPath = filepath.Join(home, ".local", "share", "enhanced-history", "hist.db")
	} else if !filepath.IsAbs(dbPath) {
		return nil, fmt.Errorf("ENHANCED_HISTORY_DB must be an absolute path, got %q", dbPath)
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	return db.Open(dbPath)
}

func runRecord(args []string) error {
	store, err := openDB()
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer store.Close()

	return record.Run(args, store)
}

func runBackup(args []string) error {
	store, err := openDB()
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer store.Close()

	return backup.Run(args, store, os.Stdout)
}

func runTUI() error {
	store, err := openDB()
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer store.Close()

	return tui.Run(store)
}

func runQuery(args []string) error {
	store, err := openDB()
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer store.Close()

	return query.Run(args, store, os.Stdout)
}
