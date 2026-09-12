package discovery

import (
	"context"
	"fmt"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/lowply/gh-workspace-run/internal/process"
	"github.com/lowply/gh-workspace-run/internal/remote"
)

var versionPattern = regexp.MustCompile(`\b(\d+)\.(\d+)\.(\d+)\b`)

type Result struct {
	Root           string
	Repository     string
	RepositoryName string
	Codespace      string
}

func Find(ctx context.Context, runner process.Runner, cwd string) (Result, error) {
	for _, name := range []string{"gh", "git", "ssh", "rsync"} {
		if _, err := exec.LookPath(name); err != nil {
			return Result{}, fmt.Errorf("required executable %q was not found in PATH", name)
		}
	}

	versionOutput, err := runner.Output(ctx, cwd, "gh", "version")
	if err != nil {
		return Result{}, fmt.Errorf("check GitHub CLI version: %w", err)
	}
	if !supportedVersion(string(versionOutput)) {
		return Result{}, fmt.Errorf("GitHub CLI 2.62.0 or newer is required")
	}

	rootOutput, err := runner.Output(ctx, cwd, "git", "-C", cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return Result{}, fmt.Errorf("find Git worktree: %w", err)
	}
	root := strings.TrimSpace(string(rootOutput))
	if root == "" {
		return Result{}, fmt.Errorf("find Git worktree: empty top-level path")
	}

	remoteOutput, remoteErr := runner.Output(ctx, root, "git", "-C", root, "remote", "get-url", "origin")
	repository, parseErr := remote.RepositoryFromRemote(strings.TrimSpace(string(remoteOutput)))
	if remoteErr != nil || parseErr != nil {
		repositoryOutput, err := runner.Output(ctx, root, "gh", "repo", "view", "--json", "nameWithOwner", "--jq", ".nameWithOwner")
		if err != nil {
			return Result{}, fmt.Errorf("resolve GitHub repository: %w", err)
		}
		repository = strings.TrimSpace(string(repositoryOutput))
	}
	parts := strings.Split(repository, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Result{}, fmt.Errorf("resolve GitHub repository: unexpected name %q", repository)
	}

	return Result{
		Root:           root,
		Repository:     repository,
		RepositoryName: path.Base(repository),
	}, nil
}

func supportedVersion(output string) bool {
	match := versionPattern.FindStringSubmatch(output)
	if match == nil {
		return false
	}
	version := [3]int{}
	for i := range version {
		version[i], _ = strconv.Atoi(match[i+1])
	}
	minimum := [3]int{2, 62, 0}
	for i := range version {
		if version[i] != minimum[i] {
			return version[i] > minimum[i]
		}
	}
	return true
}
