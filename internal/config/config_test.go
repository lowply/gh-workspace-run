package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLookupReturnsConfiguredRepositoryMapping(t *testing.T) {
	writeConfig(t, `repositories:
  octocat/hello-world: example-codespace
`)

	got, found, err := Lookup("octocat/hello-world")
	if err != nil {
		t.Fatal(err)
	}
	if !found || got != "example-codespace" {
		t.Fatalf("codespace = %q, found = %v", got, found)
	}
}

func TestLookupTreatsMissingFileAndMappingAsNotFound(t *testing.T) {
	tests := []struct {
		name    string
		content *string
	}{
		{name: "missing file"},
		{name: "missing mapping", content: pointer("repositories:\n  other/repo: space\n")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.content == nil {
				t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			} else {
				writeConfig(t, *tt.content)
			}

			got, found, err := Lookup("octocat/hello-world")
			if err != nil {
				t.Fatal(err)
			}
			if found || got != "" {
				t.Fatalf("codespace = %q, found = %v", got, found)
			}
		})
	}
}

func TestLookupRejectsMalformedConfiguration(t *testing.T) {
	writeConfig(t, "repositories: [")

	_, _, err := Lookup("octocat/hello-world")

	if err == nil || !strings.Contains(err.Error(), "parse configuration") {
		t.Fatalf("error = %v", err)
	}
}

func TestSetCreatesConfigurationWithMapping(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)

	path, err := Set("octocat/hello-world", "example-codespace")

	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(root, "gh-workspace-run", "config.yml")
	if path != wantPath {
		t.Fatalf("path = %q, want %q", path, wantPath)
	}
	assertMappings(t, path, map[string]string{
		"octocat/hello-world": "example-codespace",
	})
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("directory mode = %o, want 700", got)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("file mode = %o, want 600", got)
	}
}

func TestSetPreservesExistingMappings(t *testing.T) {
	writeConfig(t, "repositories:\n  other/repo: other-space\n")

	path, err := Set("octocat/hello-world", "example-codespace")

	if err != nil {
		t.Fatal(err)
	}
	assertMappings(t, path, map[string]string{
		"other/repo":          "other-space",
		"octocat/hello-world": "example-codespace",
	})
}

func TestSetDoesNotOverwriteMalformedConfiguration(t *testing.T) {
	const malformed = "repositories: ["
	writeConfig(t, malformed)
	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}

	_, err = Set("octocat/hello-world", "example-codespace")

	if err == nil || !strings.Contains(err.Error(), "parse configuration") {
		t.Fatalf("error = %v", err)
	}
	content, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != malformed {
		t.Fatalf("configuration changed to %q", content)
	}
}

func assertMappings(t *testing.T, path string, want map[string]string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config fileConfig
	if err := yaml.Unmarshal(content, &config); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(config.Repositories, want) {
		t.Fatalf("repositories = %#v, want %#v", config.Repositories, want)
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
