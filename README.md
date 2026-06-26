# enhanced-history

Enhanced shell history for bash — records every command with timestamp, working directory, and session ID, stored in SQLite.

## Build

```bash
make build    # produces ./hist
make install  # copies hist to ~/bin/hist
```

## Usage

```bash
hist                      # list all history (history-style output)
hist --since "2026-06-01" # filter by date
hist --dir ~/projects/foo # filter by directory
hist --session <id>       # filter by session
hist --search <keyword>   # full-text search
```

## Shell Integration

To hook enhanced-history into bash, install [bash-preexec](https://github.com/rcaloras/bash-preexec) and then add to `~/.bashrc`:

```bash
source /path/to/bash-preexec.sh
source /path/to/enhanced-history.bash
```

This will:
- Set `EH_SESSION` to a UUID identifying the current shell session (preserved across re-sources)
- Define a `preexec` hook that records every command with its working directory and session ID
- Do nothing silently if the `hist` binary is not on `PATH`

## Development

```bash
make test   # run tests
make vet    # run go vet
```
