package syncer

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/lowply/gh-workspace-run/internal/connection"
	"github.com/lowply/gh-workspace-run/internal/process"
	"github.com/lowply/gh-workspace-run/internal/remote"
)

func TestDifferenceReturnsSortedRemoteOnlyPaths(t *testing.T) {
	got := difference(
		[]string{"new", "keep", "keep"},
		[]string{"z-old", "keep", "a-old", "z-old"},
	)
	want := []string{"a-old", "z-old"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("difference = %#v, want %#v", got, want)
	}
}

func TestRunTransfersThenDeletesRemoteOnlyPaths(t *testing.T) {
	fixture := installSyncTools(t, false)
	syncer := fixture.syncer()

	if err := syncer.Run(context.Background(), fixture.localRoot, "/workspaces/project"); err != nil {
		t.Fatal(err)
	}
	if got := readSyncFile(t, fixture.events); got != "list\nrsync\ndelete\n" {
		t.Fatalf("events = %q", got)
	}
	if got := readSyncBytes(t, fixture.rsyncInput); !bytes.Equal(got, []byte("keep\x00new\x00")) {
		t.Fatalf("rsync stdin = %q", got)
	}
	if got := readSyncBytes(t, fixture.deleteInput); !bytes.Equal(got, []byte("old\x00")) {
		t.Fatalf("delete stdin = %q", got)
	}
	args := readSyncFile(t, fixture.rsyncArgs)
	for _, want := range []string{
		"--from0",
		"--files-from=-",
		"--relative",
		"--protect-args",
		"ssh -F '/tmp/config path'",
		fixture.localRoot + "/",
		"silver-space:/workspaces/project/",
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("rsync args missing %q: %s", want, args)
		}
	}
}

func TestRunDoesNotDeleteWhenRsyncFails(t *testing.T) {
	fixture := installSyncTools(t, true)

	err := fixture.syncer().Run(context.Background(), fixture.localRoot, "/workspaces/project")
	if err == nil || !strings.Contains(err.Error(), "sync files") {
		t.Fatalf("error = %v", err)
	}
	if got := readSyncFile(t, fixture.events); got != "list\nrsync\n" {
		t.Fatalf("events = %q", got)
	}
	if _, err := os.Stat(fixture.deleteInput); !os.IsNotExist(err) {
		t.Fatalf("delete was invoked: %v", err)
	}
}

type syncFixture struct {
	localRoot   string
	events      string
	rsyncInput  string
	rsyncArgs   string
	deleteInput string
	runner      process.Runner
	client      remote.Client
}

func (f syncFixture) syncer() Syncer {
	return Syncer{Runner: f.runner, Remote: f.client}
}

func installSyncTools(t *testing.T, rsyncFails bool) syncFixture {
	t.Helper()
	dir := t.TempDir()
	localRoot := t.TempDir()
	events := filepath.Join(dir, "events")
	rsyncInput := filepath.Join(dir, "rsync-input")
	rsyncArgs := filepath.Join(dir, "rsync-args")
	deleteInput := filepath.Join(dir, "delete-input")

	writeSyncExecutable(t, dir, "git", `#!/bin/sh
printf 'keep\0new\0'
`)
	rsyncExit := "0"
	if rsyncFails {
		rsyncExit = "23"
	}
	writeSyncExecutable(t, dir, "rsync", `#!/bin/sh
printf 'rsync\n' >> "$EVENTS"
printf '%s\n' "$*" > "$RSYNC_ARGS"
cat > "$RSYNC_INPUT"
exit `+rsyncExit+`
`)
	writeSyncExecutable(t, dir, "ssh", `#!/bin/sh
case "$*" in
  *"git ls-files"*)
    printf 'list\n' >> "$EVENTS"
    printf 'keep\0old\0'
    ;;
  *"xargs -0"*)
    printf 'delete\n' >> "$EVENTS"
    cat > "$DELETE_INPUT"
    ;;
  *) exit 9 ;;
esac
`)

	t.Setenv("EVENTS", events)
	t.Setenv("RSYNC_INPUT", rsyncInput)
	t.Setenv("RSYNC_ARGS", rsyncArgs)
	t.Setenv("DELETE_INPUT", deleteInput)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	runner := process.Runner{Stdout: io.Discard, Stderr: io.Discard}
	client := remote.Client{
		Runner: runner,
		Config: connection.Config{Path: "/tmp/config path", Host: "silver-space"},
	}
	return syncFixture{
		localRoot:   localRoot,
		events:      events,
		rsyncInput:  rsyncInput,
		rsyncArgs:   rsyncArgs,
		deleteInput: deleteInput,
		runner:      runner,
		client:      client,
	}
}

func writeSyncExecutable(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func readSyncFile(t *testing.T, path string) string {
	t.Helper()
	return string(readSyncBytes(t, path))
}

func readSyncBytes(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}
