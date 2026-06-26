package query

import (
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
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
	me := fs.Bool("me", false, "filter by current session ($EH_SESSION)")
	search := fs.String("search", "", "substring match on command text")
	full := fs.Bool("full", false, "verbose output: id, cmd, dir, date (UTC), session")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}

	if *me && *session != "" {
		return fmt.Errorf("--me and --session are mutually exclusive")
	}

	var f Filter

	if *since != "" {
		t, err := time.Parse("2006-01-02", *since)
		if err != nil {
			return fmt.Errorf("invalid --since date %q: must be YYYY-MM-DD", *since)
		}
		f.TsFrom = t.UTC().Unix()
	}

	if *me {
		ehSession := os.Getenv("EH_SESSION")
		if ehSession == "" {
			return fmt.Errorf("$EH_SESSION is not set — is the enhanced-history shell integration loaded?")
		}
		f.Session = ehSession
	} else {
		f.Session = *session
	}

	f.Dir = *dir
	f.CmdSubstr = *search

	rows, err := store.Query(f)
	if err != nil {
		return fmt.Errorf("query history: %w", err)
	}

	w := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
	for _, r := range rows {
		if *full {
			ts := time.Unix(r.Ts, 0).UTC().Format("2006-01-02 15:04")
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n", r.ID, r.Cmd, r.Dir, ts, r.Session)
		} else {
			fmt.Fprintf(w, "%d\t%s\n", r.ID, r.Cmd)
		}
	}
	return w.Flush()
}
