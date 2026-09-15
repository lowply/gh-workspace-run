package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/lowply/gh-workspace-run/internal/agentguide"
	"github.com/lowply/gh-workspace-run/internal/config"
	"github.com/lowply/gh-workspace-run/internal/connection"
	"github.com/lowply/gh-workspace-run/internal/discovery"
	"github.com/lowply/gh-workspace-run/internal/process"
	"github.com/lowply/gh-workspace-run/internal/remote"
	"github.com/lowply/gh-workspace-run/internal/syncer"
)

const usage = `usage: gh workspace-run [--sync] [--remote-dir PATH] [--persist DURATION] [--verbose] [-- COMMAND [ARG...]]
       gh workspace-run config
       gh workspace-run agent-guide
       gh workspace-run flush

options:
  --sync              Synchronize local files before finishing or running
  --remote-dir PATH   Override /workspaces/<repository-name>
  --persist DURATION  Override the 3h SSH connection persistence
  --verbose           Print subprocess commands and rsync statistics
  -h, --help          Show help`

type options struct {
	sync           bool
	helpOnly       bool
	configOnly     bool
	agentGuideOnly bool
	flushOnly      bool
	remoteDir      string
	persist        time.Duration
	verbose        bool
	command        []string
}

type usageError struct {
	err error
}

var displaySafe = regexp.MustCompile(`^[A-Za-z0-9_./:@%+=,-]+$`)

func (e usageError) Error() string {
	return e.err.Error()
}

func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	opts, err := parseArgs(args)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%v\n%s\n", err, usage)
		return 2
	}
	if opts.helpOnly {
		_, _ = fmt.Fprintln(stdout, usage)
		return 0
	}
	if opts.agentGuideOnly {
		configPath, err := config.Path()
		if err != nil {
			return fail(stderr, err)
		}
		_, _ = io.WriteString(stdout, strings.Replace(agentguide.Content(), "{{CONFIG_PATH}}", configPath, 1))
		printCodespaceSuggestion(ctx, stdout)
		return 0
	}
	if opts.configOnly {
		path, created, err := config.Ensure()
		if err != nil {
			return fail(stderr, err)
		}
		if created {
			_, _ = fmt.Fprintf(stdout, "Created config: %s\n", path)
		} else {
			_, _ = fmt.Fprintf(stdout, "Config already exists: %s\n", path)
		}
		return 0
	}
	if opts.flushOnly {
		cacheDir, removed, err := connection.Cleanup()
		if err != nil {
			return fail(stderr, err)
		}
		_, _ = fmt.Fprintf(stdout, "Cache: %s\n", cacheDir)
		for _, name := range removed {
			_, _ = fmt.Fprintf(stdout, "Removed: %s\n", name)
		}
		_, _ = fmt.Fprintf(stdout, "Purged %d cache files.\n", len(removed))
		return 0
	}

	runner := process.Runner{
		Stdin:   stdin,
		Stdout:  stdout,
		Stderr:  stderr,
		Verbose: opts.verbose,
		Debug:   os.Getenv("DEBUG") == "1",
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fail(stderr, fmt.Errorf("find current directory: %w", err))
	}
	discovered, err := discovery.Find(ctx, runner, cwd)
	if err != nil {
		return fail(stderr, err)
	}
	remoteDir := opts.remoteDir
	if remoteDir == "" {
		remoteDir = path.Join("/workspaces", discovered.RepositoryName)
	}
	codespace, err := resolveCodespace(
		ctx,
		runner,
		discovered.Root,
		discovered.Repository,
		stdout,
	)
	if err != nil {
		return fail(stderr, err)
	}
	if _, _, err := connection.Cleanup(30 * 24 * time.Hour); err != nil {
		return fail(stderr, err)
	}
	discovered.Codespace = codespace
	sshConfig, err := connection.Prepare(ctx, runner, discovered.Codespace, opts.persist)
	if err != nil {
		return fail(stderr, err)
	}
	client := remote.Client{Runner: runner, Config: sshConfig}
	if err := client.ValidateRepository(ctx, remoteDir, discovered.Repository); err != nil {
		return fail(stderr, err)
	}

	if opts.sync {
		_, _ = fmt.Fprintf(stdout, "Syncing to %s...\n", discovered.Codespace)
		synchronizer := syncer.Syncer{Runner: runner, Remote: client}
		if err := synchronizer.Run(ctx, discovered.Root, remoteDir); err != nil {
			return fail(stderr, err)
		}
		if len(opts.command) == 0 {
			return 0
		}
	}

	_, _ = fmt.Fprintf(stdout, "Running: %s\n", displayCommand(opts.command))
	if err := client.Run(ctx, remoteDir, opts.command); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		return fail(stderr, fmt.Errorf("run remote command: %w", err))
	}
	return 0
}

func fail(stderr io.Writer, err error) int {
	_, _ = fmt.Fprintln(stderr, err)
	return 1
}

func resolveCodespace(
	ctx context.Context,
	runner process.Runner,
	cwd string,
	repository string,
	stdout io.Writer,
) (string, error) {
	codespace, found, err := config.Lookup(repository)
	if err != nil {
		return "", err
	}
	if found {
		return codespace, nil
	}

	codespaces, err := listCodespaces(ctx, runner, cwd, repository)
	if err != nil {
		return "", err
	}
	switch len(codespaces) {
	case 0:
		return "", fmt.Errorf("no Codespaces found for repository %s", repository)
	case 1:
		if codespaces[0] == "" {
			return "", fmt.Errorf("Codespace for repository %s has an empty name", repository)
		}
	default:
		return "", fmt.Errorf(
			"multiple Codespaces found for repository %s: %s",
			repository,
			strings.Join(codespaces, ", "),
		)
	}

	configPath, err := config.Set(repository, codespaces[0])
	if err != nil {
		return "", err
	}
	_, _ = fmt.Fprintf(
		stdout,
		"Added config mapping: %s -> %s in %s\n",
		repository,
		codespaces[0],
		configPath,
	)
	return codespaces[0], nil
}

func listCodespaces(
	ctx context.Context,
	runner process.Runner,
	cwd string,
	repository string,
) ([]string, error) {
	output, err := runner.Output(ctx, cwd, "gh", "codespace", "list", "--repo", repository, "--json", "name")
	if err != nil {
		return nil, fmt.Errorf("list Codespaces for repository %s: %w", repository, err)
	}
	var values []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(output, &values); err != nil {
		return nil, fmt.Errorf("list Codespaces for repository %s: parse response: %w", repository, err)
	}
	codespaces := make([]string, len(values))
	for i, value := range values {
		codespaces[i] = value.Name
	}
	return codespaces, nil
}

func printCodespaceSuggestion(ctx context.Context, stdout io.Writer) {
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	runner := process.Runner{}
	repositoryOutput, err := runner.Output(ctx, cwd, "gh", "repo", "view", "--json", "nameWithOwner", "--jq", ".nameWithOwner")
	if err != nil {
		return
	}
	repository := strings.TrimSpace(string(repositoryOutput))
	codespaces, err := listCodespaces(ctx, runner, cwd, repository)
	if err != nil {
		return
	}
	if len(codespaces) != 1 || codespaces[0] == "" {
		return
	}
	_, _ = fmt.Fprintf(stdout, `
## Suggested mapping

`+"`gh codespace list --repo %s`"+` returned exactly one Codespace:

`+"```yaml"+`
repositories:
  %s: %s
`+"```"+`
`, repository, repository, codespaces[0])
}

func displayCommand(command []string) string {
	parts := make([]string, len(command))
	for i, arg := range command {
		if displaySafe.MatchString(arg) {
			parts[i] = arg
		} else {
			parts[i] = remote.Quote(arg)
		}
	}
	return strings.Join(parts, " ")
}

func parseArgs(args []string) (options, error) {
	opts := options{persist: 3 * time.Hour}
	if len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "-h")) {
		opts.helpOnly = true
		return opts, nil
	}
	if args[0] == "config" {
		if len(args) != 1 {
			return options{}, usageError{errors.New("config does not accept arguments or options")}
		}
		opts.configOnly = true
		return opts, nil
	}
	if args[0] == "agent-guide" {
		if len(args) != 1 {
			return options{}, usageError{errors.New("agent-guide does not accept arguments or options")}
		}
		opts.agentGuideOnly = true
		return opts, nil
	}
	if args[0] == "flush" {
		if len(args) != 1 {
			return options{}, usageError{errors.New("flush does not accept arguments or options")}
		}
		opts.flushOnly = true
		return opts, nil
	}

	flags := flag.NewFlagSet("workspace-run", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.BoolVar(&opts.sync, "sync", false, "")
	flags.StringVar(&opts.remoteDir, "remote-dir", "", "")
	flags.DurationVar(&opts.persist, "persist", opts.persist, "")
	flags.BoolVar(&opts.verbose, "verbose", false, "")

	separator := -1
	for i, arg := range args {
		if arg == "--" {
			separator = i
			break
		}
	}
	flagArgs := args
	if separator >= 0 {
		flagArgs = args[:separator]
	}
	if err := flags.Parse(flagArgs); err != nil {
		return options{}, usageError{err}
	}
	if opts.persist <= 0 {
		return options{}, usageError{errors.New("--persist must be greater than zero")}
	}
	if flags.NArg() != 0 {
		return options{}, usageError{errors.New("remote command must follow --")}
	}
	if separator >= 0 {
		opts.command = append([]string(nil), args[separator+1:]...)
		if len(opts.command) == 0 {
			return options{}, usageError{errors.New("remote command is required after --")}
		}
	} else if !opts.sync {
		return options{}, usageError{errors.New("either --sync or a remote command is required")}
	}
	return opts, nil
}
