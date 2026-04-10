# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Is

Attuine is a dev orchestration CLI for multi-repo projects. It wraps Docker Compose (profiles, services, hooks) and multi-repo git operations (branch, sync, status) behind a single config file (`attuine.yml`). Running without a subcommand launches an interactive TUI (Bubble Tea).

## Build / Test / Lint

```sh
make build          # -> bin/attuine
make test           # go test ./... -v
make lint           # go vet ./...
go test ./internal/docker/ -run TestParseStatus -v   # single test
```

Requires Go 1.26+ (pinned in `mise.toml`).

## Architecture

```
cmd/attuine/
  main.go            # Entrypoint — calls cli.Execute()
  cli/
    root.go          # Cobra root command, config loading, TUI launch
    profile.go       # profile up/down/list — Docker Compose profile management
    service.go       # service list — service status
    branch.go        # branch create/checkout/list — multi-repo git branching
    sync.go          # sync — fetch + checkout default branch + pull across repos
    status.go        # status — git status across repos
    commit_subs.go   # commit-subs — stage and commit submodule pointer changes
    output.go        # JSON/text output helpers shared by all commands
    man.go           # Hidden man page generator

internal/
  config/            # Loads attuine.yml, walks up directories to find it
  docker/            # Docker Compose wrapper (up/down/stop/build/logs/exec/status)
  git/               # Git operations (branch, fetch, pull, checkout, status, ahead/behind)
  runner/            # Runs arbitrary host shell commands with streaming output
  state/             # Persists last-active-profile to ~/.local/state/attuine/state.json
  tui/               # Bubble Tea TUI — two-panel layout (sidebar + output)
    model.go         # Top-level Model, routes messages to active View
    view.go          # View interface (Services view, Git view)
    service_view.go  # Services view — Docker service management + project commands
    git_view.go      # Git view — repo status and branch operations
    keys.go          # Key bindings
    styles.go        # Lipgloss styles
```

**Key patterns:**
- Config discovery walks up from cwd looking for `attuine.yml`. All paths in config are resolved relative to its directory.
- CLI commands all support `--json` for machine-readable output.
- `--repo` flag filters multi-repo commands to specific repos.
- `errPartialSuccess` (exit code 2) signals some-but-not-all repos succeeded.
- TUI uses a `View` interface (`service_view.go`, `git_view.go`) — the `Model` delegates rendering and updates to the active view.
- Docker Compose operations stream output via channels (`<-chan string`). The TUI and CLI both consume these.

## Config File Format

`attuine.yml` defines: `compose_file`, `compose_env`, `hooks.pre_up`, `profiles` (named sets of compose profiles), `projects` (with commands), and `repos` (with paths and default branches).
