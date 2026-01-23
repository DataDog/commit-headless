package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// Logger provides structured logging with GitHub Actions workflow command support.
type Logger struct {
	w            io.Writer
	actions      bool
	githubOutput string
}

// NewLogger creates a logger that writes to w. If running in GitHub Actions
// (detected via GITHUB_ACTIONS env var), it writes to stdout instead (required
// for workflow commands) and emits workflow commands for grouping and annotations.
func NewLogger(w io.Writer) *Logger {
	actions := os.Getenv("GITHUB_ACTIONS") == "true"
	githubOutput := os.Getenv("GITHUB_OUTPUT")
	if actions {
		w = os.Stdout
	}
	return &Logger{
		w:            w,
		actions:      actions,
		githubOutput: githubOutput,
	}
}

// Printf writes a formatted message to the log.
func (l *Logger) Printf(f string, args ...any) {
	fmt.Fprintf(l.w, f, args...)
}

// Group starts a collapsible group in GitHub Actions logs.
// Returns a function that must be called to end the group.
// Usage:
//
//	end := logger.Group("Processing files")
//	defer end()
func (l *Logger) Group(title string) func() {
	if l.actions {
		fmt.Fprintf(l.w, "::group::%s\n", title)
		return func() { fmt.Fprintln(l.w, "::endgroup::") }
	}
	fmt.Fprintf(l.w, "%s\n", title)
	return func() {}
}

// Notice emits an informational annotation in GitHub Actions,
// or a regular log message otherwise.
func (l *Logger) Notice(msg string) {
	if l.actions {
		fmt.Fprintf(l.w, "::notice::%s\n", msg)
	} else {
		fmt.Fprintf(l.w, "%s\n", msg)
	}
}

// Noticef emits a formatted notice.
func (l *Logger) Noticef(f string, args ...any) {
	l.Notice(fmt.Sprintf(f, args...))
}

// Warning emits a warning annotation in GitHub Actions,
// or a prefixed log message otherwise.
func (l *Logger) Warning(msg string) {
	if l.actions {
		fmt.Fprintf(l.w, "::warning::%s\n", msg)
	} else {
		fmt.Fprintf(l.w, "warning: %s\n", msg)
	}
}

// Warningf emits a formatted warning.
func (l *Logger) Warningf(f string, args ...any) {
	l.Warning(fmt.Sprintf(f, args...))
}

// Error emits an error annotation in GitHub Actions,
// or a prefixed log message otherwise.
func (l *Logger) Error(msg string) {
	if l.actions {
		fmt.Fprintf(l.w, "::error::%s\n", msg)
	} else {
		fmt.Fprintf(l.w, "error: %s\n", msg)
	}
}

// Errorf emits a formatted error.
func (l *Logger) Errorf(f string, args ...any) {
	l.Error(fmt.Sprintf(f, args...))
}

// Output writes a value that should be captured by the caller.
// In GitHub Actions, this writes to GITHUB_OUTPUT file. Otherwise, it prints to stdout.
// The name parameter is used as the output variable name in Actions.
func (l *Logger) Output(name, value string) error {
	if l.githubOutput != "" {
		f, err := os.OpenFile(l.githubOutput, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
		if err != nil {
			return fmt.Errorf("open GITHUB_OUTPUT: %w", err)
		}
		defer f.Close()

		// Use heredoc syntax for multiline-safe output
		delim := randomDelimiter()
		_, err = fmt.Fprintf(f, "%s<<%s\n%s\n%s\n", name, delim, value, delim)
		return err
	}

	// Outside Actions, just print to stdout for capture
	fmt.Println(value)
	return nil
}

func randomDelimiter() string {
	b := make([]byte, 16)
	rand.Read(b)
	return "delim_" + hex.EncodeToString(b)
}
