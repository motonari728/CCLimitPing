# AGENTS.md

Instructions for coding agents working in this repository.

## Build to `bin/` when you finish a change

Every time you finish changing code, build the binary into `bin/` before you
report the work as done:

```sh
go build -o bin/limitping ./cmd/limitping
```

`bin/` is gitignored — it is the local build the maintainer runs by hand, so a
stale binary there means testing yesterday's code without noticing. `bin/lmp` is
a symlink to `bin/limitping`; rebuilding in place keeps it valid, so do not
delete or recreate it.

This is not a substitute for `go test ./...` — build to `bin/` in addition to
whatever verification the change calls for, not instead of it.
