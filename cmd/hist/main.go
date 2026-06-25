package main

import (
	"fmt"
	"os"
	"path/filepath"

	"enhanced-history/internal/db"
	"enhanced-history/internal/record"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: hist <sub-command> [flags]")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "record":
		if err := runRecord(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "hist record: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "hist: unknown sub-command %q\n", os.Args[1])
		os.Exit(1)
	}
}

func runRecord(args []string) error {
	dbPath := os.Getenv("ENHANCED_HISTORY_DB")
	if dbPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve home dir: %w", err)
		}
		dbPath = filepath.Join(home, ".local", "share", "enhanced-history", "hist.db")
	} else if !filepath.IsAbs(dbPath) {
		return fmt.Errorf("ENHANCED_HISTORY_DB must be an absolute path, got %q", dbPath)
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return fmt.Errorf("create db directory: %w", err)
	}

	store, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer store.Close()

	return record.Run(args, store)
}
