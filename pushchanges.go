package main

import (
	"context"
	"fmt"
	"strings"
)

// pushChanges pushes a list of changes using the given client.
// headSha is the resolved parent for the first commit.
// If the client has CreateBranch behavior needed, the caller should have already handled that.
func pushChanges(ctx context.Context, client *Client, headSha string, changes ...Change) error {
	hashes := []string{}
	for i := 0; i < len(changes) && i < 10; i++ {
		hashes = append(hashes, changes[i].hash)
	}

	if len(changes) >= 10 {
		hashes = append(hashes, fmt.Sprintf("...and %d more.", len(changes)-10))
	}

	endGroup := logger.Group(fmt.Sprintf("Pushing to %s/%s (branch: %s)", client.owner, client.repo, client.branch))
	defer endGroup()

	logger.Printf("Commits: %s\n", strings.Join(hashes, ", "))
	logger.Printf("Remote head commit: %s\n", headSha)

	pushed, newHead, err := client.PushChanges(ctx, headSha, changes...)
	if err != nil {
		return err
	} else if pushed != len(changes) {
		return fmt.Errorf("pushed %d of %d changes", pushed, len(changes))
	}

	logger.Noticef("Pushed %d commit(s): %s", len(changes), client.compareURL(headSha, newHead))

	// Output the new head reference for capture by callers or GitHub Actions
	if err := logger.Output("pushed_ref", newHead); err != nil {
		return fmt.Errorf("write output: %w", err)
	}

	return nil
}
