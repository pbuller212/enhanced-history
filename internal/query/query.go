package query

import (
	"flag"
	"fmt"
	"io"
	"time"

	"enhanced-history/internal/record"
)

// Filter holds the optional constraints for a history query.
type Filter struct {
	TsFrom    int64
	TsTo      int64
	Dir       string
	Session   string
	CmdSubstr string
}

// Querier is satisfied by db.DB; defined here to avoid an import cycle.
type Querier interface {
	Query(f Filter) ([]record.Record, error)
}

// Run parses args, builds a Filter, queries store, and writes history-style
// output to out. Errors are returned to the caller; nothing is written to out
// when an error occurs.
func Run(args []string, store Querier, out io.Writer) error {
	fs := flag.NewFlagSet("hist", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	since := fs.String("since", "", "show commands on or after this date (YYYY-MM-DD)")
	dir := fs.String("dir", "", "prefix match on working directory")
	session := fs.String("session", "", "exact match on session ID")
	search := fs.String("search", "", "substring match on command text")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}

	var f Filter

	if *since != "" {
		t, err := time.Parse("2006-01-02", *since)
		if err != nil {
			return fmt.Errorf("invalid --since date %q: must be YYYY-MM-DD", *since)
		}
		f.TsFrom = t.UTC().Unix()
	}

	f.Dir = *dir
	f.Session = *session
	f.CmdSubstr = *search

	rows, err := store.Query(f)
	if err != nil {
		return fmt.Errorf("query history: %w", err)
	}

	for _, r := range rows {
		fmt.Fprintf(out, "%5d  %s\n", r.ID, r.Cmd)
	}
	return nil
}
