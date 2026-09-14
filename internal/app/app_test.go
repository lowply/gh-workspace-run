package app

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRunUsage(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "flush rejects options", args: []string{"flush", "--sync"}, wantErr: "flush does not accept arguments or options"},
		{name: "config rejects command", args: []string{"config", "--", "true"}, wantErr: "config does not accept arguments or options"},
		{name: "agent guide rejects arguments", args: []string{"agent-guide", "--verbose"}, wantErr: "agent-guide does not accept arguments or options"},
		{name: "options without operation", args: []string{"--verbose"}, wantErr: "either --sync or a remote command is required"},
		{name: "separator without command", args: []string{"--sync", "--"}, wantErr: "remote command is required after --"},
		{name: "command without separator", args: []string{"true"}, wantErr: "remote command must follow --"},
		{name: "invalid persistence", args: []string{"--persist", "forever", "--", "true"}, wantErr: "invalid value"},
		{name: "non-positive persistence", args: []string{"--persist", "0s", "--", "true"}, wantErr: "--persist must be greater than zero"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			status := Run(context.Background(), tt.args, nil, io.Discard, &stderr)
			if status != 2 {
				t.Fatalf("status = %d, want 2", status)
			}
			if !strings.Contains(stderr.String(), "usage:") {
				t.Fatalf("stderr = %q, want usage", stderr.String())
			}
			if !strings.Contains(stderr.String(), "gh workspace-run") {
				t.Fatalf("stderr = %q, want renamed command", stderr.String())
			}
			if !strings.Contains(stderr.String(), tt.wantErr) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), tt.wantErr)
			}
		})
	}
}

func TestRunHelpWithoutExternalTools(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for _, args := range [][]string{nil, {"--help"}, {"-h"}} {
		var stdout, stderr bytes.Buffer

		status := Run(context.Background(), args, nil, &stdout, &stderr)

		if status != 0 {
			t.Fatalf("args = %q, status = %d, stderr = %q", args, status, stderr.String())
		}
		if !strings.Contains(stdout.String(), "usage: gh workspace-run") {
			t.Fatalf("args = %q, stdout = %q, want help", args, stdout.String())
		}
		for _, option := range []string{"--sync", "--remote-dir PATH", "--persist DURATION", "--verbose", "-h, --help"} {
			if !strings.Contains(stdout.String(), option) {
				t.Fatalf("args = %q, stdout = %q, want option %q", args, stdout.String(), option)
			}
		}
		if stderr.Len() != 0 {
			t.Fatalf("args = %q, stderr = %q, want empty", args, stderr.String())
		}
	}
}

func TestRunAgentGuideWithoutExternalTools(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	configRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	var stdout, stderr bytes.Buffer

	status := Run(context.Background(), []string{"agent-guide"}, nil, &stdout, &stderr)

	if status != 0 {
		t.Fatalf("status = %d, stderr = %q", status, stderr.String())
	}
	guide := stdout.String()
	for _, want := range []string{
		"# gh-workspace-run Agent Guide",
		"gh workspace-run --sync",
		"gh workspace-run -- COMMAND [ARG...]",
		"gh workspace-run agent-guide",
		filepath.Join(configRoot, "gh-workspace-run", "config.yml"),
		"repositories:",
		"working-tree contents, not Git metadata",
	} {
		if !strings.Contains(guide, want) {
			t.Fatalf("stdout = %q, want %q", guide, want)
		}
	}
	if strings.Contains(guide, "Suggested mapping") {
		t.Fatalf("stdout = %q, want no contextual suggestion", guide)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunAgentGuideSuggestsOnlyMatchingCodespace(t *testing.T) {
	dir := t.TempDir()
	configRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	writeAppExecutable(t, dir, "gh", `#!/bin/sh
case "$1 $2" in
  "repo view") printf 'lowply/project\n' ;;
  "codespace list") printf '[{"name":"silver-space"}]\n' ;;
  *) exit 9 ;;
esac
`)
	t.Setenv("PATH", dir)
	var stdout, stderr bytes.Buffer

	status := Run(context.Background(), []string{"agent-guide"}, nil, &stdout, &stderr)

	if status != 0 {
		t.Fatalf("status = %d, stderr = %q", status, stderr.String())
	}
	guide := stdout.String()
	for _, want := range []string{
		"## Suggested mapping",
		"lowply/project: silver-space",
		"gh codespace list --repo lowply/project",
	} {
		if !strings.Contains(guide, want) {
			t.Fatalf("stdout = %q, want %q", guide, want)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunAgentGuideOmitsSuggestionForMultipleCodespaces(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeAppExecutable(t, dir, "gh", `#!/bin/sh
case "$1 $2" in
  "repo view") printf 'lowply/project\n' ;;
  "codespace list") printf '[{"name":"silver-space"},{"name":"gold-space"}]\n' ;;
  *) exit 9 ;;
esac
`)
	t.Setenv("PATH", dir)
	var stdout, stderr bytes.Buffer

	status := Run(context.Background(), []string{"agent-guide"}, nil, &stdout, &stderr)

	if status != 0 {
		t.Fatalf("status = %d, stderr = %q", status, stderr.String())
	}
	if strings.Contains(stdout.String(), "Suggested mapping") {
		t.Fatalf("stdout = %q, want no contextual suggestion", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestParseDefaults(t *testing.T) {
	got, err := parseArgs([]string{"--", "script/test", "test/example_test.rb"})
	if err != nil {
		t.Fatal(err)
	}
	if got.persist.String() != "3h0m0s" {
		t.Fatalf("persist = %s, want 3h0m0s", got.persist)
	}
	if strings.Join(got.command, " ") != "script/test test/example_test.rb" {
		t.Fatalf("command = %q", got.command)
	}
}

func TestParseSync(t *testing.T) {
	got, err := parseArgs([]string{"--sync", "--remote-dir", "/workspaces/custom", "--verbose"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.sync || len(got.command) != 0 || got.remoteDir != "/workspaces/custom" || !got.verbose {
		t.Fatalf("options = %#v", got)
	}
}

func TestParseSyncWithCommand(t *testing.T) {
	got, err := parseArgs([]string{"--sync", "--", "script/test"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.sync || strings.Join(got.command, " ") != "script/test" {
		t.Fatalf("options = %#v", got)
	}
}

func TestRunConfigCreatesTemplateWithoutExternalTools(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	configRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	var stdout, stderr bytes.Buffer

	status := Run(context.Background(), []string{"config"}, nil, &stdout, &stderr)

	if status != 0 {
		t.Fatalf("status = %d, stderr = %q", status, stderr.String())
	}
	path := filepath.Join(configRoot, "gh-workspace-run", "config.yml")
	if want := "Created config: " + path + "\n"; stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "repositories:\n  owner/repository: codespace-name\n"; string(content) != want {
		t.Fatalf("config = %q, want %q", content, want)
	}
}

func TestRunConfigPreservesExistingFile(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	configRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	path := filepath.Join(configRoot, "gh-workspace-run", "config.yml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	const existing = "repositories:\n  octocat/hello-world: existing-space\n"
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer

	status := Run(context.Background(), []string{"config"}, nil, &stdout, &stderr)

	if status != 0 {
		t.Fatalf("status = %d, stderr = %q", status, stderr.String())
	}
	if want := "Config already exists: " + path + "\n"; stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != existing {
		t.Fatalf("config changed to %q", content)
	}
}

func TestRunFlushPurgesCacheWithoutExternalTools(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	cacheDir := filepath.Join(cacheRoot, "gh-workspace-run")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config-old", "cm-old", "keep-me"} {
		if err := os.WriteFile(filepath.Join(cacheDir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var stdout, stderr bytes.Buffer

	status := Run(context.Background(), []string{"flush"}, nil, &stdout, &stderr)

	if status != 0 {
		t.Fatalf("status = %d, stderr = %q", status, stderr.String())
	}
	want := "Cache: " + cacheDir + "\n" +
		"Removed: cm-old\n" +
		"Removed: config-old\n" +
		"Purged 2 cache files.\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "keep-me")); err != nil {
		t.Fatalf("unrelated cache file: %v", err)
	}
}

func TestRunWithoutSyncExecutesCommandOnly(t *testing.T) {
	fixture := installWorkflowTools(t, 0, false)
	var stdout, stderr bytes.Buffer

	status := Run(
		context.Background(),
		[]string{"--", "script/test", "test/a b.rb"},
		nil,
		&stdout,
		&stderr,
	)

	if status != 0 {
		t.Fatalf("status = %d, stderr = %q", status, stderr.String())
	}
	if got := fixture.events(); got != "run\n" {
		t.Fatalf("events = %q", got)
	}
	if strings.Contains(stdout.String(), "Syncing to ") ||
		!strings.Contains(stdout.String(), "Running: script/test") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunWithSyncSynchronizesThenExecutesCommand(t *testing.T) {
	fixture := installWorkflowTools(t, 0, false)
	var stdout, stderr bytes.Buffer

	status := Run(
		context.Background(),
		[]string{"--sync", "--", "script/test", "test/a b.rb"},
		nil,
		&stdout,
		&stderr,
	)

	if status != 0 {
		t.Fatalf("status = %d, stderr = %q", status, stderr.String())
	}
	if got := fixture.events(); got != "rsync\ndelete\nrun\n" {
		t.Fatalf("events = %q", got)
	}
	if !strings.Contains(stdout.String(), "Syncing to silver-space...\n") ||
		!strings.Contains(stdout.String(), "Running: script/test") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestSyncDoesNotExecuteUserCommand(t *testing.T) {
	fixture := installWorkflowTools(t, 0, false)
	var stderr bytes.Buffer

	status := Run(context.Background(), []string{"--sync"}, nil, io.Discard, &stderr)

	if status != 0 {
		t.Fatalf("status = %d, stderr = %q", status, stderr.String())
	}
	if got := fixture.events(); got != "rsync\ndelete\n" {
		t.Fatalf("events = %q", got)
	}
}

func TestRunEnablesProcessTimingsWithDebugEnvironment(t *testing.T) {
	installWorkflowTools(t, 0, false)
	t.Setenv("DEBUG", "1")
	var stderr bytes.Buffer

	status := Run(context.Background(), []string{"--sync"}, nil, io.Discard, &stderr)

	if status != 0 {
		t.Fatalf("status = %d, stderr = %q", status, stderr.String())
	}
	if !strings.Contains(stderr.String(), "+ gh version") ||
		!strings.Contains(stderr.String(), "debug: ") {
		t.Fatalf("stderr = %q, want commands and timings", stderr.String())
	}
}

func TestRunUsesConfiguredCodespaceWithoutListingCodespaces(t *testing.T) {
	fixture := installWorkflowTools(t, 0, false)

	if status := Run(context.Background(), []string{"--sync"}, nil, io.Discard, io.Discard); status != 0 {
		t.Fatalf("status = %d", status)
	}

	ghCalls, err := os.ReadFile(fixture.ghLog)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(ghCalls), "codespace list\n") {
		t.Fatalf("codespace list was called:\n%s", ghCalls)
	}
}

func TestRunDoesNotExecuteAfterRsyncFailure(t *testing.T) {
	fixture := installWorkflowTools(t, 0, true)
	var stderr bytes.Buffer

	status := Run(context.Background(), []string{"--sync", "--", "script/test"}, nil, io.Discard, &stderr)

	if status != 1 {
		t.Fatalf("status = %d, stderr = %q", status, stderr.String())
	}
	if got := fixture.events(); got != "rsync\n" {
		t.Fatalf("events = %q", got)
	}
}

func TestRunReturnsRemoteExitStatus(t *testing.T) {
	installWorkflowTools(t, 37, false)
	var stderr bytes.Buffer

	status := Run(context.Background(), []string{"--", "script/test"}, nil, io.Discard, &stderr)

	if status != 37 {
		t.Fatalf("status = %d, stderr = %q", status, stderr.String())
	}
}

func TestRunUsesRemoteDirectoryOverride(t *testing.T) {
	fixture := installWorkflowTools(t, 0, false)

	status := Run(
		context.Background(),
		[]string{"--remote-dir", "/custom/worktree", "--", "true"},
		nil,
		io.Discard,
		io.Discard,
	)

	if status != 0 {
		t.Fatalf("status = %d", status)
	}
	sshLog, err := os.ReadFile(fixture.sshLog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sshLog), "/custom/worktree") {
		t.Fatalf("ssh log = %q", sshLog)
	}
}

type workflowFixture struct {
	eventLog string
	ghLog    string
	sshLog   string
}

func (f workflowFixture) events() string {
	content, _ := os.ReadFile(f.eventLog)
	return string(content)
}

func installWorkflowTools(t *testing.T, remoteExit int, rsyncFails bool) workflowFixture {
	t.Helper()
	dir := t.TempDir()
	root := t.TempDir()
	eventLog := filepath.Join(dir, "events")
	ghLog := filepath.Join(dir, "gh-log")
	sshLog := filepath.Join(dir, "ssh-log")
	t.Setenv("HOME", t.TempDir())
	configDir := filepath.Join(t.TempDir(), "gh-workspace-run")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(configDir, "config.yml"),
		[]byte("repositories:\n  lowply/project: silver-space\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Dir(configDir))
	t.Setenv("EVENT_LOG", eventLog)
	t.Setenv("GH_LOG", ghLog)
	t.Setenv("SSH_LOG", sshLog)
	t.Setenv("REMOTE_EXIT", strconv.Itoa(remoteExit))

	writeAppExecutable(t, dir, "git", `#!/bin/sh
if [ "$3" = "rev-parse" ]; then
	printf '%s\n' "$PROJECT_ROOT"
elif [ "$3" = "ls-files" ]; then
	printf 'keep\0new\0'
elif [ "$3" = "remote" ]; then
	printf 'git@github.com:lowply/project.git\n'
else
	exit 9
fi
`)
	t.Setenv("PROJECT_ROOT", root)
	writeAppExecutable(t, dir, "gh", `#!/bin/sh
printf '%s %s\n' "$1" "$2" >> "$GH_LOG"
case "$1 $2" in
  "version ") printf 'gh version 2.100.0 (test)\n' ;;
  "repo view") printf 'lowply/project\n' ;;
  "codespace list") printf '[{"name":"silver-space"}]\n' ;;
  "codespace ssh") cat <<'EOF'
Host silver-space
	User vscode
	ProxyCommand gh cs ssh -c silver-space --stdio
EOF
    ;;
  *) exit 9 ;;
esac
`)
	writeAppExecutable(t, dir, "ssh", `#!/bin/sh
printf '%s\n' "$*" >> "$SSH_LOG"
if [ "$1" = "-G" ]; then
	printf 'controlpath %s\n' "$CONTROL_PATH"
	exit 0
fi
if [ "$1" = "-O" ]; then
	exit 255
fi
case "$*" in
  *"remote get-url"*) printf 'git@github.com:lowply/project.git\n' ;;
  *"git ls-files"*) printf 'keep\0old\0' ;;
  *"xargs -0"*) printf 'delete\n' >> "$EVENT_LOG"; cat >/dev/null ;;
  *"exec "*) printf 'run\n' >> "$EVENT_LOG"; exit "$REMOTE_EXIT" ;;
  *) exit 9 ;;
esac
`)
	t.Setenv("CONTROL_PATH", filepath.Join(dir, "missing.sock"))
	rsyncExit := "0"
	if rsyncFails {
		rsyncExit = "23"
	}
	writeAppExecutable(t, dir, "rsync", "#!/bin/sh\nprintf 'rsync\\n' >> \"$EVENT_LOG\"\ncat >/dev/null\nexit "+rsyncExit+"\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return workflowFixture{eventLog: eventLog, ghLog: ghLog, sshLog: sshLog}
}

func writeAppExecutable(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
