package agentguide

import (
	"strings"
	"testing"
)

func TestContentReturnsEmbeddedGuide(t *testing.T) {
	content := Content()

	for _, want := range []string{
		"# gh-workspace-run Agent Guide",
		"{{CONFIG_PATH}}",
		"gh workspace-run agent-guide",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("Content() = %q, want %q", content, want)
		}
	}
}
