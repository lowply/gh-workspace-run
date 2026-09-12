package process

import (
	"bytes"
	"context"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestRunnerOutput(t *testing.T) {
	runner := Runner{Stderr: io.Discard}
	output, err := runner.Output(
		context.Background(),
		"",
		os.Args[0],
		"-test.run=TestHelperProcess",
		"--",
		"output",
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != "hello\n" {
		t.Fatalf("output = %q", output)
	}
}

func TestRunnerRunAttachesStreams(t *testing.T) {
	var stdout bytes.Buffer
	runner := Runner{Stdout: &stdout, Stderr: io.Discard}
	err := runner.Run(
		context.Background(),
		"",
		strings.NewReader("input"),
		os.Args[0],
		"-test.run=TestHelperProcess",
		"--",
		"copy",
	)
	if err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "input" {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunnerDebugLogsCommandAndElapsedTime(t *testing.T) {
	var stderr bytes.Buffer
	runner := Runner{Stderr: &stderr, Debug: true}

	if _, err := runner.Output(
		context.Background(),
		"",
		os.Args[0],
		"-test.run=TestHelperProcess",
		"--",
		"output",
	); err != nil {
		t.Fatal(err)
	}

	log := stderr.String()
	if !strings.Contains(log, "+ "+os.Args[0]) {
		t.Fatalf("debug log missing command: %q", log)
	}
	if !regexp.MustCompile(`(?m)^debug: \S+ \(ok\) `).MatchString(log) {
		t.Fatalf("debug log missing elapsed time: %q", log)
	}
}

func TestHelperProcess(t *testing.T) {
	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		return
	}

	switch os.Args[separator+1] {
	case "output":
		_, _ = os.Stdout.WriteString("hello\n")
	case "copy":
		_, _ = io.Copy(os.Stdout, os.Stdin)
	default:
		os.Exit(3)
	}
	os.Exit(0)
}
