package agentguide

import _ "embed"

//go:embed guide.md
var content string

func Content() string {
	return content
}
