# enhanced-history

Enhanced shell history for bash — records every command with its timestamp, working directory, and session ID into a local SQLite database. Query it with flexible filters to find what you ran, where, and when.

## Prerequisites

- **Go 1.25+** — to build the binary
- **[bash-preexec](https://github.com/rcaloras/bash-preexec)** — provides the `preexec` hook used by the shell integration
- **`uuidgen`** — generates session IDs (standard on Linux and macOS)
- **`staticcheck`** — required only for `make vet` (`go install honnef.co/go/tools/cmd/staticcheck@latest`)

## Installation

```bash
make build    # produces ./hist (static binary, CGO_ENABLED=0)
make install  # copies hist to ~/bin/hist
```

Ensure `~/bin` is on your `PATH`:

```bash
export PATH="$HOME/bin:$PATH"   # add to ~/.bashrc to make it permanent
```

To use a custom database location, set `ENHANCED_HISTORY_DB` to an absolute path before running `hist`:

```bash
export ENHANCED_HISTORY_DB=/path/to/my/history.db
```

## Bash Integration

Install [bash-preexec](https://github.com/rcaloras/bash-preexec), then add the following to your `~/.bashrc`:

```bash
source /path/to/bash-preexec.sh
source /path/to/enhanced-history.bash
```

This will:

- Set `EH_SESSION` to a UUID identifying the current shell session (preserved across re-sources, so reloading `~/.bashrc` mid-session does not start a new logical session)
- Define a `preexec` hook that records every command with its working directory and session ID
- Do nothing silently if the `hist` binary is not on `PATH` — the shell is never broken by a missing binary

## Usage

```bash
hist --help                       # print usage and exit
hist --version                    # print the version and exit
hist                              # list all history
hist --since 2026-06-01           # commands on or after this date (YYYY-MM-DD)
hist --dir ~/projects/foo         # commands run under this directory (prefix match)
hist --session <uuid>             # commands from a specific shell session
hist --me                         # commands from the current shell session ($EH_SESSION)
hist --here                       # commands run in the current directory ($PWD, exact match)
hist --search <keyword>           # substring match on command text
hist --full                       # verbose output: id, cmd, dir, date (UTC), session
```

Flags can be combined:

```bash
hist --since 2026-06-01 --dir ~/projects --search git
hist --full --since 2026-06-01 --search git
```

Output format:

```
  <id>  <command>
```

With `--full`:

```
  <id>  <command>   <dir>   <YYYY-MM-DD HH:MM>   <session-uuid>
```

Timestamps in `--full` output are in UTC.

### `hist record` (internal)

The shell integration calls this sub-command automatically — you do not need to invoke it manually:

```bash
hist record --cmd "ls -la" --dir /home/user --session <uuid> [--ts <epoch>]
```

### `hist backup`

Back up the history database:

```bash
hist backup                          # write to $HOME/<user>_<host>_<YYYYMMDD>.db
hist backup /path/to/file.db         # write to an explicit path
hist backup --format sql             # plain-text SQL dump instead of a binary copy
hist backup --force /path/to/file.db # overwrite an existing destination
```

By default `hist backup` refuses to overwrite an existing destination file; pass `--force` to allow it. On success the absolute path of the new file is printed to stdout:

```
Backup written to /home/user/user_host_20260727.db
```

## Database Location

By default the database is stored at:

```
~/.local/share/enhanced-history/hist.db
```

The directory is created automatically on first run. Override with `ENHANCED_HISTORY_DB` (must be an absolute path).

## Development

```bash
make test   # go test ./...
make vet    # go vet ./... && staticcheck ./...
make clean  # remove the ./hist build artifact
```
