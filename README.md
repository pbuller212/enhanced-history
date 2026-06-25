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

## Development

```bash
make test   # run tests
make vet    # run go vet
```
