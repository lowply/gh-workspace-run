package connection

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lowply/gh-workspace-run/internal/process"
)

func TestPrepareCachesGeneratedConfigWithMultiplexing(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home with space")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	installConnectionTools(t, `Host silver-space
	User vscode
	ProxyCommand gh cs ssh -c silver-space --stdio
`, "")

	got, err := Prepare(
		context.Background(),
		process.Runner{Stderr: io.Discard},
		"silver-space",
		3*time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "silver-space" {
		t.Fatalf("host = %q", got.Host)
	}
	content, err := os.ReadFile(got.Path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, want := range []string{
		"Host silver-space",
		"ControlMaster auto",
		"ControlPersist 3h0m0s",
		`ControlPath "`,
		"ProxyCommand gh cs ssh -c silver-space --stdio",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("config missing %q:\n%s", want, text)
		}
	}
	info, err := os.Stat(got.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
}

func TestPrepareRejectsConfigWithoutConcreteHost(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	installConnectionTools(t, "Host *\n\tUser vscode\n", "")

	_, err := Prepare(context.Background(), process.Runner{Stderr: io.Discard}, "silver-space", 3*time.Hour)
	if err == nil || !strings.Contains(err.Error(), "concrete Host") {
		t.Fatalf("error = %v", err)
	}
}

func TestPrepareRemovesReportedStaleControlSocket(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	socket := filepath.Join(t.TempDir(), "stale.sock")
	if err := os.WriteFile(socket, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	installConnectionTools(t, "Host silver-space\n\tUser vscode\n", socket)

	if _, err := Prepare(context.Background(), process.Runner{Stderr: io.Discard}, "silver-space", 3*time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Fatalf("stale socket still exists: %v", err)
	}
}

func TestPrepareReusesCachedConfigForLiveControlConnection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	ghLog := filepath.Join(dir, "gh.log")
	live := filepath.Join(dir, "live")
	controlPath := filepath.Join(dir, "control.sock")
	t.Setenv("GH_LOG", ghLog)
	t.Setenv("LIVE_CONTROL", live)
	t.Setenv("CONTROL_PATH", controlPath)
	writeConnectionExecutable(t, dir, "gh", `#!/bin/sh
printf 'generate\n' >> "$GH_LOG"
printf 'Host silver-space\n\tUser vscode\n'
`)
	writeConnectionExecutable(t, dir, "ssh", `#!/bin/sh
if [ "$1" = "-G" ]; then
	printf 'controlpath %s\n' "$CONTROL_PATH"
	exit 0
fi
if [ "$1" = "-O" ] && [ -f "$LIVE_CONTROL" ]; then
	exit 0
fi
exit 255
`)
	t.Setenv("PATH", dir)

	first, err := Prepare(context.Background(), process.Runner{Stderr: io.Discard}, "silver-space", 3*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(live, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := Prepare(context.Background(), process.Runner{Stderr: io.Discard}, "silver-space", 3*time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	if first != second {
		t.Fatalf("cached config = %#v, want %#v", second, first)
	}
	if got := readConnectionFile(t, ghLog); got != "generate\n" {
		t.Fatalf("gh config generations = %q, want one", got)
	}
}

func TestCleanupRemovesCacheEntriesOlderThanThirtyDays(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cacheDir, err := cacheDirectory()
	if err != nil {
		t.Fatal(err)
	}
	oldConfig := cachePath(cacheDir, "old-space")
	oldSocket := socketPath(cacheDir, "old-space")
	recentConfig := cachePath(cacheDir, "recent-space")
	recentSocket := socketPath(cacheDir, "recent-space")
	unrelated := filepath.Join(cacheDir, "keep-me")
	for _, path := range []string{oldConfig, oldSocket, recentConfig, recentSocket, unrelated} {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-31 * 24 * time.Hour)
	recent := time.Now().Add(-29 * 24 * time.Hour)
	for _, path := range []string{oldConfig, oldSocket, unrelated} {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{recentConfig, recentSocket} {
		if err := os.Chtimes(path, recent, recent); err != nil {
			t.Fatal(err)
		}
	}

	path, removed, err := Cleanup(30 * 24 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if path != cacheDir {
		t.Fatalf("cache path = %q, want %q", path, cacheDir)
	}
	wantRemoved := []string{filepath.Base(oldSocket), filepath.Base(oldConfig)}
	if !reflect.DeepEqual(removed, wantRemoved) {
		t.Fatalf("removed = %#v, want %#v", removed, wantRemoved)
	}

	for _, path := range []string{recentConfig, recentSocket, unrelated} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("kept path %s: %v", path, err)
		}
	}
	for _, path := range []string{oldConfig, oldSocket} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("stale path remains %s: %v", path, err)
		}
	}
}

func TestCleanupWithoutAgeRemovesAllCacheEntries(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cacheDir, err := cacheDirectory()
	if err != nil {
		t.Fatal(err)
	}
	config := cachePath(cacheDir, "space")
	socket := socketPath(cacheDir, "space")
	unrelated := filepath.Join(cacheDir, "keep-me")
	for _, path := range []string{config, socket, unrelated} {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	path, removed, err := Cleanup()
	if err != nil {
		t.Fatal(err)
	}
	if path != cacheDir {
		t.Fatalf("cache path = %q, want %q", path, cacheDir)
	}
	wantRemoved := []string{filepath.Base(socket), filepath.Base(config)}
	if !reflect.DeepEqual(removed, wantRemoved) {
		t.Fatalf("removed = %#v, want %#v", removed, wantRemoved)
	}
	for _, path := range []string{config, socket} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("cache path remains %s: %v", path, err)
		}
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("unrelated cache path: %v", err)
	}
}

func installConnectionTools(t *testing.T, generatedConfig, controlPath string) {
	t.Helper()
	dir := t.TempDir()
	writeConnectionExecutable(t, dir, "gh", "#!/bin/sh\nprintf '%s' '"+strings.ReplaceAll(generatedConfig, "'", "'\"'\"'")+"'\n")
	writeConnectionExecutable(t, dir, "ssh", `#!/bin/sh
if [ "$1" = "-G" ]; then
	printf 'controlpath %s\n' "$CONTROL_PATH"
	exit 0
fi
if [ "$1" = "-O" ]; then
	exit 255
fi
exit 9
`)
	t.Setenv("CONTROL_PATH", controlPath)
	t.Setenv("PATH", dir)
}

func writeConnectionExecutable(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func readConnectionFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
