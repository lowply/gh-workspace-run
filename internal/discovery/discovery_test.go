package discovery

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lowply/gh-workspace-run/internal/process"
)

func TestFindResolvesRepository(t *testing.T) {
	root := installTools(t, "2.100.0")

	got, err := Find(context.Background(), process.Runner{Stderr: io.Discard}, root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Root != root {
		t.Fatalf("root = %q", got.Root)
	}
	if got.Repository != "lowply/project" || got.RepositoryName != "project" {
		t.Fatalf("repository = %#v", got)
	}
}

func TestFindResolvesGitHubOriginWithoutGhRepoView(t *testing.T) {
	dir := t.TempDir()
	root := t.TempDir()
	ghLog := filepath.Join(dir, "gh.log")
	t.Setenv("GH_LOG", ghLog)
	writeExecutable(t, dir, "git", `#!/bin/sh
if [ "$3" = "rev-parse" ]; then
	printf '%s\n' "$PROJECT_ROOT"
elif [ "$3" = "remote" ]; then
	printf 'git@github.com:lowply/project.git\n'
else
	exit 9
fi
`)
	t.Setenv("PROJECT_ROOT", root)
	writeExecutable(t, dir, "gh", `#!/bin/sh
case "$1 $2" in
  "version ") printf 'gh version 2.100.0 (test)\n' ;;
  "repo view") printf 'repo-view\n' >> "$GH_LOG"; printf 'lowply/project\n' ;;
  *) exit 9 ;;
esac
`)
	writeExecutable(t, dir, "ssh", "#!/bin/sh\nexit 0\n")
	writeExecutable(t, dir, "rsync", "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", dir)

	got, err := Find(context.Background(), process.Runner{Stderr: io.Discard}, root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Repository != "lowply/project" {
		t.Fatalf("repository = %q", got.Repository)
	}
	if content, err := os.ReadFile(ghLog); err == nil {
		t.Fatalf("gh repo view was called: %q", content)
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestFindRejectsOldGitHubCLI(t *testing.T) {
	root := installTools(t, "2.61.0")

	_, err := Find(context.Background(), process.Runner{Stderr: io.Discard}, root)
	if err == nil || !strings.Contains(err.Error(), "2.62.0 or newer") {
		t.Fatalf("error = %v", err)
	}
}

func TestFindRejectsMissingPrerequisite(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, dir, "gh", "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", dir)

	_, err := Find(context.Background(), process.Runner{Stderr: io.Discard}, dir)
	if err == nil || !strings.Contains(err.Error(), "required executable") {
		t.Fatalf("error = %v", err)
	}
}

func installTools(t *testing.T, version string) string {
	t.Helper()
	dir := t.TempDir()
	root := t.TempDir()
	writeExecutable(t, dir, "git", `#!/bin/sh
if [ "$1" = "-C" ] && [ "$3" = "rev-parse" ]; then
	printf '%s\n' '`+root+`'
	exit 0
fi
exit 9
`)
	writeExecutable(t, dir, "gh", `#!/bin/sh
case "$1 $2" in
  "version ") printf 'gh version `+version+` (test)\n' ;;
  "repo view") printf 'lowply/project\n' ;;
  *) exit 9 ;;
esac
`)
	writeExecutable(t, dir, "ssh", "#!/bin/sh\nexit 0\n")
	writeExecutable(t, dir, "rsync", "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", dir)
	return root
}

func writeExecutable(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
