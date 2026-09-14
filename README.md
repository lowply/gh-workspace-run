# gh-workspace-run

`gh-workspace-run` lets you edit a Git repository locally and run commands in that
repository's GitHub Codespace.

```console
$ gh workspace-run -- script/test
Running: script/test
```

Run commands against the existing remote workspace, or synchronize local files
first. When synchronizing, local files are authoritative: the extension sends
tracked files and untracked, non-ignored files to the Codespace with `rsync`,
removes remote-only Git-visible files, and leaves remote ignored files and
`.git` untouched.

## Requirements

- GitHub CLI 2.62.0 or newer
- Git
- OpenSSH
- rsync
- Exactly one Codespace for the current GitHub repository
- An SSH server in the Codespace

For Debian-based devcontainers, SSH can be installed with:

```json
{
  "features": {
    "ghcr.io/devcontainers/features/sshd:1": {
      "version": "latest"
    }
  }
}
```

## Install

Install the published extension:

```console
gh extension install lowply/gh-workspace-run
```

Install the current checkout while developing:

```console
go build -o gh-workspace-run .
gh extension install .
```

Upgrade a published installation:

```console
gh extension upgrade workspace-run
```

## Usage

| Invocation | Behavior | Exit status |
| --- | --- | --- |
| `gh workspace-run` | Show help | `0` |
| `gh workspace-run --help` | Show help | `0` |
| `gh workspace-run -h` | Show help | `0` |
| `gh workspace-run --sync` | Synchronize only | `0` on success |
| `gh workspace-run -- COMMAND [ARG...]` | Run without synchronizing | Remote command status |
| `gh workspace-run --sync -- COMMAND [ARG...]` | Synchronize, then run | Sync failure or remote command status |
| `gh workspace-run config` | Create or report the configuration file | `0` on success |
| `gh workspace-run agent-guide` | Print a detailed Markdown guide for agents | `0` |
| `gh workspace-run flush` | Purge all cached files and sockets | `0` on success |

Options must appear before `--`:

```text
--sync              Synchronize local files before finishing or running
--remote-dir PATH   Override /workspaces/<repository-name>
--persist DURATION  Override the 3h SSH connection persistence
--verbose           Print subprocess commands and rsync statistics
-h, --help          Show help
```

Examples:

```console
gh workspace-run -- script/test
gh workspace-run -- script/test test/jobs/example_test.rb
gh workspace-run --sync -- script/test
gh workspace-run --sync --verbose
gh workspace-run --remote-dir /workspaces/custom --persist 30m -- script/test
```

`config`, `agent-guide`, and `flush` do not accept options or arguments.
Invocations that do not request synchronization or provide a command are also
rejected:

```console
gh workspace-run flush --sync
gh workspace-run config -- COMMAND
gh workspace-run --verbose
gh workspace-run --sync --
gh workspace-run COMMAND
```

Everything after `--` belongs to the remote command, even when it resembles an
extension option.

The `config` command creates
`${XDG_CONFIG_HOME:-$HOME/.config}/gh-workspace-run/config.yml`
when it does not exist and never overwrites an existing file. Edit the generated
mapping:

```yaml
repositories:
  octocat/hello-world: example-codespace
```

Every repository must be mapped to its Codespace name. The command fails rather
than searching for an unmapped Codespace.

Print a detailed Markdown guide for coding agents:

```console
gh workspace-run agent-guide
```

The guide includes the resolved configuration path. When the current repository
can be resolved and `gh codespace list --repo OWNER/REPOSITORY` returns exactly
one Codespace, it also includes a copyable repository mapping. Context lookup is
best-effort and never prevents the guide from being printed.

Purge all cached SSH configuration files and control sockets:

```console
gh workspace-run flush
```

Set `DEBUG=1` to print each external command and its elapsed execution time:

```console
DEBUG=1 gh workspace-run --sync
```

## How it works

1. Resolve the current Git worktree and GitHub repository from its `origin`,
   falling back to GitHub CLI when needed.
2. Read the repository's Codespace name from
   `${XDG_CONFIG_HOME:-~/.config}/gh-workspace-run/config.yml`.
3. Generate supported OpenSSH configuration with `gh codespace ssh --config`.
4. Configure OpenSSH connection multiplexing with `ControlPersist 3h`.
5. When synchronization is requested, compare
   `git ls-files -co --exclude-standard -z` locally and remotely, transfer the
   local file set with rsync, and delete remote Git-visible paths that no
   longer exist locally.
6. When running, execute the requested command from the remote workspace
   directory.

The first invocation establishes the Codespaces SSH tunnel. Later invocations
reuse the OpenSSH control connection for up to three hours after the last use.
Cached SSH configuration files and control sockets older than 30 days are
removed automatically when the command runs.
Stop the Codespace explicitly when finished if you do not want it kept
available:

```console
gh codespace stop
```

## Important limitation

The extension synchronizes working-tree contents, not Git metadata. The remote
`.git` directory retains the Codespace's original `HEAD`, refs, and index.
Builds and tests see local file contents, but commands such as `git status`
inside the Codespace may not describe the local branch accurately.

## Troubleshooting

**No Codespaces found**

Create a Codespace for the current repository, then rerun the command.

**Multiple Codespaces found**

Version 1 intentionally does not guess. Stop or remove the unused Codespaces so
exactly one remains for the repository.

**SSH server is missing**

Add the `sshd` devcontainer feature shown above and rebuild the Codespace.

**Authentication or tunnel errors**

Run `gh auth status`, then verify that `gh codespace ssh -c <name>` connects.

**A required executable is missing**

Install the executable named in the error. `gh-workspace-run` delegates GitHub access,
file discovery, tunneling, and incremental transfer to `gh`, `git`, `ssh`, and
`rsync` rather than reimplementing those protocols.
