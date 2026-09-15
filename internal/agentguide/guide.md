# gh-workspace-run Agent Guide

Use this GitHub CLI extension to run commands in the Codespace mapped to the current repository, optionally synchronizing local working-tree files first.

## Commands

| Command | Behavior |
| --- | --- |
| `gh workspace-run` | Show concise help |
| `gh workspace-run --sync` | Synchronize files without running a command |
| `gh workspace-run -- COMMAND [ARG...]` | Run a command without synchronizing |
| `gh workspace-run --sync -- COMMAND [ARG...]` | Synchronize, then run a command |
| `gh workspace-run config` | Create the configuration file if missing |
| `gh workspace-run agent-guide` | Print this Markdown guide |
| `gh workspace-run flush` | Purge cached SSH files and sockets |

Options must appear before `--`:

- `--sync` synchronizes before finishing or running.
- `--remote-dir PATH` overrides `/workspaces/<repository-name>`.
- `--persist DURATION` overrides the default three-hour SSH connection persistence.
- `--verbose` prints subprocess commands and rsync statistics.

## Configuration

Configuration file: `{{CONFIG_PATH}}`

Map each GitHub repository to its Codespace name:

```yaml
repositories:
  owner/repository: codespace-name
```

If a repository is not mapped and exactly one Codespace exists for it, the
command adds the mapping automatically and reports the config update. With zero
or multiple Codespaces, add the desired mapping manually after inspecting
`gh codespace list --repo OWNER/REPOSITORY`.

## Behavior and constraints

- Local tracked files and untracked, non-ignored files are synchronized with `rsync`.
- Local files are authoritative; remote-only Git-visible files are removed.
- Remote ignored files and `.git` are preserved.
- Synchronization copies working-tree contents, not Git metadata, so remote Git status may not describe the local branch.
- Commands run from the repository workspace directory and return the remote command's exit status.
- The Codespace must run an SSH server.
