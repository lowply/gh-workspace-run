package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodespaceReturnsConfiguredRepositoryMapping(t *testing.T) {
	writeConfig(t, `repositories:
  octocat/hello-world: example-codespace
`)

	got, err := Codespace("octocat/hello-world")
	if err != nil {
		t.Fatal(err)
	}
	if got != "example-codespace" {
		t.Fatalf("codespace = %q", got)
	}
}

func TestCodespaceReportsConfigurationErrors(t *testing.T) {
	tests := []struct {
		name    string
		content *string
		want    string
	}{
		{name: "missing file", want: "config.yml"},
		{name: "malformed YAML", content: pointer("repositories: ["), want: "parse"},
		{name: "unmapped repository", content: pointer("repositories:\n  other/repo: space\n"), want: "octocat/hello-world"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.content == nil {
				t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			} else {
				writeConfig(t, *tt.content)
			}

			_, err := Codespace("octocat/hello-world")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func writeConfig(t *testing.T, content string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	dir := filepath.Join(root, "gh-workspace-run")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func pointer(value string) *string {
	return &value
}
