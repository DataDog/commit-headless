package main

import (
	"fmt"
	"strings"
)

// Change represents a single change that will be pushed to the remote.
type Change struct {
	hash   string
	author string

	message string

	// trailers are k/v pairs added to the end of a commit message
	// stored as a list to maintain insertion order
	trailers [][2]string

	// entries is a map of path -> content for files modified in the change
	// empty or nil content indicates a deleted file
	entries map[string][]byte
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
	sb := &strings.Builder{}

	sb.WriteString(strings.TrimSpace(b))
	sb.WriteString("\n\n")

	if c.author != "" {
		sb.WriteString(fmt.Sprintf("Co-authored-by: %s\n", c.author))
	}

	for _, t := range c.trailers {
		sb.WriteString(fmt.Sprintf("%s: %s\n", t[0], t[1]))
	}

	return strings.TrimSpace(sb.String())
}
