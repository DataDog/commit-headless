package main

import (
	"fmt"
	"strings"
)

// FileEntry represents a file in a change with its content and mode.
type FileEntry struct {
	Content []byte // nil indicates deletion, empty slice indicates empty file
	Mode    string // git file mode (e.g., "100644", "100755")
}

// Change represents a single change that will be pushed to the remote.
type Change struct {
	hash   string
	author string

	message string

	// trailers are lines to add to the end of the body stored as a list to maintain insertion order
	trailers []string

	// entries is a map of path -> FileEntry for files modified in the change
	entries map[string]FileEntry
}

// Splits a commit message on the first blank line
func (c Change) splitMessage() (string, string) {
	h, b, _ := strings.Cut(c.message, "\n\n")
	return h, b
}

// Headline is the first paragraph of the message
func (c Change) Headline() string {
	h, _ := c.splitMessage()
	return h
}

// Body is everything after the headline, including trailers
func (c Change) Body() string {
	_, b := c.splitMessage()
	b = strings.TrimSpace(b)

	sb := &strings.Builder{}
	sb.WriteString(b)
	sb.WriteString("\n\n")

	// maybe write trailers, if the trailer doesn't already exist in the body
	// this is a naive implementation, but it mostly does the job
	lowerbody := strings.ToLower(b)

	if c.author != "" {
		authorline := fmt.Sprintf("Co-authored-by: %s", c.author)
		c.trailers = append([]string{authorline}, c.trailers...)
	}

	for _, t := range c.trailers {
		if !strings.Contains(lowerbody, strings.ToLower(t)) {
			sb.WriteString(t)
			sb.WriteString("\n")
		}
	}

	return strings.TrimSpace(sb.String())
}
