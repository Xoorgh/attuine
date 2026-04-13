# Attuine

Dev orchestration CLI for multi-repo projects. Wraps Docker Compose and cross-repo git operations behind a single config file. Run without a subcommand to launch an interactive TUI.

## Build from Source

Requires Go 1.26+.

```sh
make build        # produces bin/attuine
make test         # run tests
make lint         # run go vet
```

### Install (Linux / macOS)

```sh
make install                        # installs to /usr/local/bin + man page
make install PREFIX=$HOME/.local    # custom prefix
sudo make install                   # system-wide install
```

To uninstall:

```sh
make uninstall
```

### Install (Windows)

```powershell
go build -o attuine.exe ./cmd/attuine
```

Place the resulting binary somewhere on your `PATH`. Man pages are not applicable on Windows. Docker Desktop and Git for Windows must be installed.

### Man Pages

Man pages are generated automatically during `make install`. To generate them separately:

```sh
make man          # generates man pages in doc/man/
```

This produces `doc/man/attuine.1` (and subcommand pages) from the Cobra command tree. After installing, view them with `man attuine`.

## Platform Support

| Platform | Status | Notes |
|---|---|---|
| Linux | Fully supported | Primary development platform |
| macOS | Fully supported | Requires Docker Desktop |
| Windows | Supported | Requires Docker Desktop and Git for Windows |

On Windows, host commands defined in `projects[].commands[].run` and `hooks.pre_up` are executed via `cmd /c` instead of `sh -c`. Write these commands in a portable way or use platform-appropriate scripts.

## Setup

Create an `attuine.yml` in your project root. Attuine auto-discovers it by walking up from the current directory.

### Minimal example (standalone repos)

```yaml
compose_file: docker-compose.yml

profiles:
  - name: Core
    profiles: [core]

repos:
  frontend:
    path: ./frontend
    default_branch: main
  backend:
    path: ./backend
```

### Full example (submodules)

```yaml
layout: submodules
compose_file: dev/docker-compose.yml
compose_env: dev/.env

hooks:
  pre_up:
    - name: Resolve versions
      run: ./dev/resolve-versions.sh

profiles:
  - name: Core
    profiles: [core]
  - name: Full Stack
    profiles: [full]

projects:
  myapp:
    path: ./myapp
    commands:
      - name: Tests
        run: bundle exec rspec
        service: myapp
      - name: Console
        run: bin/rails console
        service: myapp
        interactive: true

repos:
  parent:
    path: .
    default_branch: master
  myapp:
    path: ./myapp
    default_branch: master
```

### Config reference

| Field | Required | Description |
|---|---|---|
| `compose_file` | yes | Path to Docker Compose file (relative to config) |
| `compose_env` | no | Path to env file passed to Compose |
| `layout` | no | `standalone` (default) or `submodules` |
| `hooks.pre_up` | no | Shell commands run before `profile up` |
| `profiles` | no | Named sets of Compose profiles |
| `projects` | no | Projects with paths and commands |
| `repos` | no | Git repositories with paths and default branches |

Setting `layout: submodules` enables the `commit-subs` command for staging and committing submodule pointer changes. This requires a repo with `path: .` (the parent repo).

## Usage

```sh
attuine                     # launch TUI
attuine status              # git status across all repos
attuine sync                # fetch + checkout default branch + pull (skips dirty repos)
attuine branch create NAME  # create branch across repos
attuine branch checkout NAME
attuine profile up Core     # bring up a profile's services
attuine profile down        # stop all services
attuine service list        # show running services
```

All commands support `--json` for machine-readable output and `--repo` to filter to specific repos.

## Shell Completion

```sh
# Bash
attuine completion bash > /etc/bash_completion.d/attuine

# Zsh
attuine completion zsh > "${fpath[1]}/_attuine"

# Fish
attuine completion fish > ~/.config/fish/completions/attuine.fish

# PowerShell
attuine completion powershell > attuine.ps1
```

## License

[MIT](LICENSE)
