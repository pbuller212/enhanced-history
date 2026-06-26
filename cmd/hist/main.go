package main

import (
	"fmt"
	"os"
	"path/filepath"

	"enhanced-history/internal/db"
	"enhanced-history/internal/query"
	"enhanced-history/internal/record"
)

func main() {
	// With no arguments, or any arguments that don't match a sub-command,
	// default to the query path.
	if len(os.Args) >= 2 && os.Args[1] == "record" {
		if err := runRecord(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "hist record: %v\n", err)
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
	if err := runQuery(args); err != nil {
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

func runQuery(args []string) error {
	store, err := openDB()
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer store.Close()

	return query.Run(args, store, os.Stdout)
}
