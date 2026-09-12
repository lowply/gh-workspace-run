package app

import (
	"context"
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

	"github.com/lowply/gh-workspace-run/internal/config"
	"github.com/lowply/gh-workspace-run/internal/connection"
	"github.com/lowply/gh-workspace-run/internal/discovery"
	"github.com/lowply/gh-workspace-run/internal/process"
	"github.com/lowply/gh-workspace-run/internal/remote"
	"github.com/lowply/gh-workspace-run/internal/syncer"
)

const usage = "usage: gh workspace-run [--remote-dir PATH] [--persist DURATION] [--verbose] -- COMMAND [ARG...]\n       gh workspace-run sync [--remote-dir PATH] [--persist DURATION] [--verbose]\n       gh workspace-run config\n       gh workspace-run flush"

type options struct {
	syncOnly   bool
	configOnly bool
	flushOnly  bool
	remoteDir  string
	persist    time.Duration
	verbose    bool
	command    []string
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
	codespace, err := config.Codespace(discovered.Repository)
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

	_, _ = fmt.Fprintf(stdout, "Syncing to %s...\n", discovered.Codespace)
	synchronizer := syncer.Syncer{Runner: runner, Remote: client}
	if err := synchronizer.Run(ctx, discovered.Root, remoteDir); err != nil {
		return fail(stderr, err)
	}
	if opts.syncOnly {
		return 0
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
	if len(args) > 0 && args[0] == "config" {
		if len(args) != 1 {
			return options{}, usageError{errors.New("config does not accept arguments")}
		}
		opts.configOnly = true
		return opts, nil
	}
	if len(args) > 0 && args[0] == "flush" {
		if len(args) != 1 {
			return options{}, usageError{errors.New("flush does not accept arguments")}
		}
		opts.flushOnly = true
		return opts, nil
	}
	if len(args) > 0 && args[0] == "sync" {
		opts.syncOnly = true
		args = args[1:]
	}

	flags := flag.NewFlagSet("workspace-run", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&opts.remoteDir, "remote-dir", "", "")
	flags.DurationVar(&opts.persist, "persist", opts.persist, "")
	flags.BoolVar(&opts.verbose, "verbose", false, "")

	if opts.syncOnly {
		if err := flags.Parse(args); err != nil {
			return options{}, usageError{err}
		}
		if opts.persist <= 0 {
			return options{}, usageError{errors.New("--persist must be greater than zero")}
		}
		if flags.NArg() != 0 {
			return options{}, usageError{errors.New("sync does not accept positional arguments")}
		}
		return opts, nil
	}

	separator := -1
	for i, arg := range args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 {
		return options{}, usageError{errors.New("remote command must follow --")}
	}
	if err := flags.Parse(args[:separator]); err != nil {
		return options{}, usageError{err}
	}
	if opts.persist <= 0 {
		return options{}, usageError{errors.New("--persist must be greater than zero")}
	}
	if flags.NArg() != 0 {
		return options{}, usageError{fmt.Errorf("unexpected argument %q", strings.Join(flags.Args(), " "))}
	}
	opts.command = append([]string(nil), args[separator+1:]...)
	if len(opts.command) == 0 {
		return options{}, usageError{errors.New("remote command is required")}
	}
	return opts, nil
}
