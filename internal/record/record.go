package record

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"time"
)

// Inserter is satisfied by db.DB; defined here to avoid an import cycle.
type Inserter interface {
	Insert(r Record) error
}

// Record is a single history entry.
type Record struct {
	ID      int64
	Ts      int64
	Dir     string
	Session string
	Cmd     string
}

// Run parses args, validates them, and writes one record via store.
// It is the entry point for the "record" sub-command.
func Run(args []string, store Inserter) error {
	fs := flag.NewFlagSet("record", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cmd := fs.String("cmd", "", "command string (required)")
	dir := fs.String("dir", "", "working directory (required)")
	session := fs.String("session", "", "session ID (required)")
	ts := fs.Int64("ts", 0, "unix epoch timestamp (default: now)")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}

	if *cmd == "" {
		return errors.New("--cmd is required and must not be empty")
	}
	if *dir == "" {
		return errors.New("--dir is required and must not be empty")
	}
	if *session == "" {
		return errors.New("--session is required and must not be empty")
	}

	epoch := *ts
	if epoch == 0 {
		epoch = time.Now().Unix()
	}

	r := Record{
		Ts:      epoch,
		Dir:     *dir,
		Session: *session,
		Cmd:     *cmd,
	}

	if err := store.Insert(r); err != nil {
		return fmt.Errorf("store record: %w", err)
	}
	return nil
}
