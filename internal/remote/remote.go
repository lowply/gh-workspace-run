package remote

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"

	"github.com/lowply/gh-workspace-run/internal/connection"
	"github.com/lowply/gh-workspace-run/internal/process"
)

type Client struct {
	Runner process.Runner
	Config connection.Config
}

func Quote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func ValidatePath(value string) error {
	if value == "" {
		return fmt.Errorf("empty repository path")
	}
	if strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("repository path contains NUL")
	}
	clean := path.Clean(value)
	if path.IsAbs(value) || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("unsafe repository path %q", value)
	}
	if clean != value {
		return fmt.Errorf("non-canonical repository path %q", value)
	}
	return nil
}

func (c Client) ValidateRepository(ctx context.Context, dir, repository string) error {
	command := fmt.Sprintf("git -C %s remote get-url origin", Quote(dir))
	output, err := c.output(ctx, command)
	if err != nil {
		return fmt.Errorf("validate remote repository: %w", err)
	}
	actual, err := RepositoryFromRemote(strings.TrimSpace(string(output)))
	if err != nil {
		return fmt.Errorf("validate remote repository: %w", err)
	}
	if actual != repository {
		return fmt.Errorf("remote repository is %s, expected %s", actual, repository)
	}
	return nil
}

func (c Client) GitVisibleFiles(ctx context.Context, dir string) ([]string, error) {
	command := fmt.Sprintf("cd -- %s && git ls-files -co --exclude-standard -z", Quote(dir))
	output, err := c.output(ctx, command)
	if err != nil {
		return nil, fmt.Errorf("list remote Git-visible files: %w", err)
	}
	files, err := parseNUL(output)
	if err != nil {
		return nil, fmt.Errorf("list remote Git-visible files: %w", err)
	}
	return files, nil
}

func (c Client) Delete(ctx context.Context, dir string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	var input bytes.Buffer
	for _, value := range paths {
		if err := ValidatePath(value); err != nil {
			return err
		}
		input.WriteString(value)
		input.WriteByte(0)
	}
	command := fmt.Sprintf("cd -- %s && xargs -0 -r rm -rf --", Quote(dir))
	if err := c.run(ctx, &input, command); err != nil {
		return fmt.Errorf("delete remote-only files: %w", err)
	}
	return nil
}

func (c Client) Run(ctx context.Context, dir string, command []string) error {
	quoted := make([]string, len(command))
	for i, arg := range command {
		quoted[i] = Quote(arg)
	}
	remoteCommand := fmt.Sprintf("cd -- %s && exec %s", Quote(dir), strings.Join(quoted, " "))
	return c.run(ctx, nil, remoteCommand)
}

func (c Client) output(ctx context.Context, command string) ([]byte, error) {
	return c.Runner.Output(ctx, "", "ssh", "-F", c.Config.Path, c.Config.Host, command)
}

func (c Client) run(ctx context.Context, stdin io.Reader, command string) error {
	return c.Runner.Run(ctx, "", stdin, "ssh", "-F", c.Config.Path, c.Config.Host, command)
}

func parseNUL(input []byte) ([]string, error) {
	if len(input) == 0 {
		return nil, nil
	}
	if input[len(input)-1] != 0 {
		return nil, fmt.Errorf("path list is not NUL-terminated")
	}
	parts := bytes.Split(input[:len(input)-1], []byte{0})
	files := make([]string, 0, len(parts))
	for _, part := range parts {
		value := string(part)
		if err := ValidatePath(value); err != nil {
			return nil, err
		}
		files = append(files, value)
	}
	return files, nil
}

func RepositoryFromRemote(remote string) (string, error) {
	remote = strings.TrimSuffix(remote, ".git")
	if strings.HasPrefix(remote, "git@github.com:") {
		repository := strings.TrimPrefix(remote, "git@github.com:")
		if validRepository(repository) {
			return repository, nil
		}
	}
	parsed, err := url.Parse(remote)
	if err == nil && strings.EqualFold(parsed.Hostname(), "github.com") {
		repository := strings.TrimPrefix(parsed.Path, "/")
		if validRepository(repository) {
			return repository, nil
		}
	}
	return "", fmt.Errorf("unsupported GitHub remote %q", remote)
}

func validRepository(repository string) bool {
	parts := strings.Split(repository, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
}
