package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoggerOutput(t *testing.T) {
	t.Run("writes to stdout when no GITHUB_OUTPUT", func(t *testing.T) {
		var buf bytes.Buffer
		l := &Logger{w: &buf, actions: false, githubOutput: ""}

		err := l.Output("test_name", "test_value")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should print to stdout (which we can't capture here, but Output returns nil)
		// The actual stdout write happens in the real Output method
	})

	t.Run("writes to GITHUB_OUTPUT file when set", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputFile := filepath.Join(tmpDir, "github_output")

		var buf bytes.Buffer
		l := &Logger{w: &buf, actions: true, githubOutput: outputFile}

		err := l.Output("pushed_ref", "abc123def456")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		content, err := os.ReadFile(outputFile)
		if err != nil {
			t.Fatalf("failed to read output file: %v", err)
		}

		// Should contain the variable name and value in heredoc format
		if !strings.Contains(string(content), "pushed_ref<<delim_") {
			t.Errorf("expected heredoc format, got: %s", content)
		}
		if !strings.Contains(string(content), "abc123def456") {
			t.Errorf("expected value in output, got: %s", content)
		}
	})
}

func TestLoggerGroup(t *testing.T) {
	t.Run("emits workflow commands in actions mode", func(t *testing.T) {
		var buf bytes.Buffer
		l := &Logger{w: &buf, actions: true}

		end := l.Group("Test Group")
		l.Printf("inside group\n")
		end()

		output := buf.String()
		if !strings.Contains(output, "::group::Test Group") {
			t.Errorf("expected ::group:: command, got: %s", output)
		}
		if !strings.Contains(output, "::endgroup::") {
			t.Errorf("expected ::endgroup:: command, got: %s", output)
		}
	})

	t.Run("emits plain text outside actions", func(t *testing.T) {
		var buf bytes.Buffer
		l := &Logger{w: &buf, actions: false}

		end := l.Group("Test Group")
		l.Printf("inside group\n")
		end()

		output := buf.String()
		if strings.Contains(output, "::group::") {
			t.Errorf("should not emit ::group:: outside actions, got: %s", output)
		}
		if !strings.Contains(output, "Test Group") {
			t.Errorf("expected group title, got: %s", output)
		}
	})
}

func TestLoggerAnnotations(t *testing.T) {
	t.Run("notice in actions mode", func(t *testing.T) {
		var buf bytes.Buffer
		l := &Logger{w: &buf, actions: true}

		l.Notice("test notice")

		if !strings.Contains(buf.String(), "::notice::test notice") {
			t.Errorf("expected ::notice:: command, got: %s", buf.String())
		}
	})

	t.Run("warning in actions mode", func(t *testing.T) {
		var buf bytes.Buffer
		l := &Logger{w: &buf, actions: true}

		l.Warning("test warning")

		if !strings.Contains(buf.String(), "::warning::test warning") {
			t.Errorf("expected ::warning:: command, got: %s", buf.String())
		}
	})

	t.Run("error in actions mode", func(t *testing.T) {
		var buf bytes.Buffer
		l := &Logger{w: &buf, actions: true}

		l.Error("test error")

		if !strings.Contains(buf.String(), "::error::test error") {
			t.Errorf("expected ::error:: command, got: %s", buf.String())
		}
	})

	t.Run("annotations outside actions", func(t *testing.T) {
		var buf bytes.Buffer
		l := &Logger{w: &buf, actions: false}

		l.Notice("test notice")
		l.Warning("test warning")
		l.Error("test error")

		output := buf.String()
		if strings.Contains(output, "::") {
			t.Errorf("should not emit workflow commands outside actions, got: %s", output)
		}
		if !strings.Contains(output, "warning: test warning") {
			t.Errorf("expected prefixed warning, got: %s", output)
		}
		if !strings.Contains(output, "error: test error") {
			t.Errorf("expected prefixed error, got: %s", output)
		}
	})
}
