package process

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

type Runner struct {
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	Verbose bool
	Debug   bool
}

func (r Runner) Output(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	r.log(name, args)
	start := time.Now()
	err := cmd.Run()
	r.logResult(name, args, start, err)
	if err != nil {
		return nil, commandError(name, err, stderr.String())
	}
	return stdout.Bytes(), nil
}

func (r Runner) Run(ctx context.Context, dir string, stdin io.Reader, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	if stdin == nil {
		stdin = r.Stdin
	}
	cmd.Stdin = stdin
	cmd.Stdout = r.Stdout
	cmd.Stderr = r.Stderr
	r.log(name, args)
	start := time.Now()
	err := cmd.Run()
	r.logResult(name, args, start, err)
	return err
}

func (r Runner) log(name string, args []string) {
	if (!r.Verbose && !r.Debug) || r.Stderr == nil {
		return
	}
	_, _ = fmt.Fprintf(r.Stderr, "+ %s\n", displayCommand(name, args))
}

func (r Runner) logResult(name string, args []string, start time.Time, err error) {
	if !r.Debug || r.Stderr == nil {
		return
	}
	status := "ok"
	if err != nil {
		status = err.Error()
	}
	_, _ = fmt.Fprintf(
		r.Stderr,
		"debug: %s (%s) %s\n",
		time.Since(start).Round(time.Microsecond),
		status,
		displayCommand(name, args),
	)
}

func displayCommand(name string, args []string) string {
	parts := append([]string{name}, args...)
	for i, part := range parts {
		if part == "" || strings.ContainsAny(part, " \t\n'\"\\$") {
			parts[i] = fmt.Sprintf("%q", part)
		}
	}
	return strings.Join(parts, " ")
}

func commandError(name string, err error, stderr string) error {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return fmt.Errorf("%s: %w", name, err)
	}
	return fmt.Errorf("%s: %w: %s", name, err, stderr)
}
