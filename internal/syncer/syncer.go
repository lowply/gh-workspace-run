package syncer

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lowply/gh-workspace-run/internal/process"
	"github.com/lowply/gh-workspace-run/internal/remote"
)

type Syncer struct {
	Runner process.Runner
	Remote remote.Client
}

func (s Syncer) Run(ctx context.Context, localRoot, remoteRoot string) error {
	localOutput, err := s.Runner.Output(
		ctx,
		localRoot,
		"git",
		"-C",
		localRoot,
		"ls-files",
		"-co",
		"--exclude-standard",
		"-z",
	)
	if err != nil {
		return fmt.Errorf("list local Git-visible files: %w", err)
	}
	localFiles, err := parseNUL(localOutput)
	if err != nil {
		return fmt.Errorf("list local Git-visible files: %w", err)
	}
	remoteFiles, err := s.Remote.GitVisibleFiles(ctx, remoteRoot)
	if err != nil {
		return err
	}

	args := []string{
		"-a",
		"--from0",
		"--files-from=-",
		"--relative",
		"--protect-args",
	}
	if s.Runner.Verbose {
		args = append(args, "--info=stats1")
	}
	args = append(args,
		"-e",
		"ssh -F "+remote.Quote(s.Remote.Config.Path),
		filepath.Clean(localRoot)+string(os.PathSeparator),
		s.Remote.Config.Host+":"+strings.TrimSuffix(remoteRoot, "/")+"/",
	)
	if err := s.Runner.Run(ctx, localRoot, bytes.NewReader(localOutput), "rsync", args...); err != nil {
		return fmt.Errorf("sync files: %w", err)
	}

	if err := s.Remote.Delete(ctx, remoteRoot, difference(localFiles, remoteFiles)); err != nil {
		return err
	}
	return nil
}

func difference(local, remoteFiles []string) []string {
	localSet := make(map[string]struct{}, len(local))
	for _, value := range local {
		localSet[value] = struct{}{}
	}
	remoteOnlySet := make(map[string]struct{})
	for _, value := range remoteFiles {
		if _, ok := localSet[value]; !ok {
			remoteOnlySet[value] = struct{}{}
		}
	}
	remoteOnly := make([]string, 0, len(remoteOnlySet))
	for value := range remoteOnlySet {
		remoteOnly = append(remoteOnly, value)
	}
	sort.Strings(remoteOnly)
	return remoteOnly
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
		if err := remote.ValidatePath(value); err != nil {
			return nil, err
		}
		files = append(files, value)
	}
	return files, nil
}
