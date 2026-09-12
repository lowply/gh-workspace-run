package remote

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/lowply/gh-workspace-run/internal/connection"
	"github.com/lowply/gh-workspace-run/internal/process"
)

func TestQuote(t *testing.T) {
	tests := map[string]string{
		"":      "''",
		"abc":   "'abc'",
		"a b":   "'a b'",
		"a'b":   `'a'"'"'b'`,
		"a\nb":  "'a\nb'",
		"$HOME": "'$HOME'",
	}
	for input, want := range tests {
		if got := Quote(input); got != want {
			t.Errorf("Quote(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestValidatePath(t *testing.T) {
	for _, path := range []string{".env.example", "test/a b.rb", "line\nbreak"} {
		if err := ValidatePath(path); err != nil {
			t.Errorf("ValidatePath(%q): %v", path, err)
		}
	}
	for _, path := range []string{"", "/etc/passwd", "..", "../a", "a/../../b", "a\x00b"} {
		if err := ValidatePath(path); err == nil {
			t.Errorf("ValidatePath(%q) succeeded", path)
		}
	}
}

func TestValidateRepository(t *testing.T) {
	client, log := testClient(t, `#!/bin/sh
printf '%s\n' "$*" > "$LOG"
printf 'git@github.com:lowply/project.git\n'
`)

	if err := client.ValidateRepository(context.Background(), "/workspaces/project", "lowply/project"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, log); !strings.Contains(got, "git -C '/workspaces/project' remote get-url origin") {
		t.Fatalf("ssh args = %q", got)
	}
}

func TestValidateRepositoryRejectsMismatch(t *testing.T) {
	client, _ := testClient(t, "#!/bin/sh\nprintf 'https://github.com/other/project.git\\n'\n")

	err := client.ValidateRepository(context.Background(), "/workspaces/project", "lowply/project")
	if err == nil || !strings.Contains(err.Error(), "other/project") {
		t.Fatalf("error = %v", err)
	}
}

func TestGitVisibleFilesPreservesNULSeparatedNames(t *testing.T) {
	client, _ := testClient(t, "#!/bin/sh\nprintf 'a\\0line\\nbreak\\0'\n")

	got, err := client.GitVisibleFiles(context.Background(), "/workspaces/project")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a", "line\nbreak"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("files = %#v, want %#v", got, want)
	}
}

func TestDeleteSendsNULSeparatedPaths(t *testing.T) {
	client, log := testClient(t, `#!/bin/sh
cat > "$LOG"
`)

	if err := client.Delete(context.Background(), "/workspaces/project", []string{"old.rb", "line\nbreak"}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte("old.rb\x00line\nbreak\x00"); !bytes.Equal(got, want) {
		t.Fatalf("stdin = %q, want %q", got, want)
	}
}

func TestDeleteRejectsUnsafePathBeforeSSH(t *testing.T) {
	client := Client{
		Runner: process.Runner{Stderr: io.Discard},
		Config: connection.Config{Path: "config", Host: "host"},
	}
	if err := client.Delete(context.Background(), "/workspaces/project", []string{"../escape"}); err == nil {
		t.Fatal("Delete succeeded")
	}
}

func TestRunPreservesSSHExitError(t *testing.T) {
	client, _ := testClient(t, "#!/bin/sh\nexit 37\n")

	err := client.Run(context.Background(), "/workspaces/project", []string{"script/test", "a b"})
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 37 {
		t.Fatalf("error = %v", err)
	}
}

func testClient(t *testing.T, script string) (Client, string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	path := filepath.Join(dir, "ssh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOG", log)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return Client{
		Runner: process.Runner{
			Stdout: io.Discard,
			Stderr: io.Discard,
		},
		Config: connection.Config{
			Path: "/tmp/config",
			Host: "silver-space",
		},
	}, log
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
