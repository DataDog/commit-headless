package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var hashRegex = regexp.MustCompile(`^[a-f0-9]{4,40}$`)

// Takes a list of changes to push to the remote identified by target.
// Prints the last commit pushed to standard output.
func pushChanges(ctx context.Context, owner, repository, branch, headSha string, createBranch, dryrun bool, changes ...Change) error {
	hashes := []string{}
	for i := 0; i < len(changes) && i < 10; i++ {
		hashes = append(hashes, changes[i].hash)
	}

	if len(changes) >= 10 {
		hashes = append(hashes, fmt.Sprintf("...and %d more.", len(changes)-10))
	}

	endGroup := logger.Group(fmt.Sprintf("Pushing to %s/%s (branch: %s)", owner, repository, branch))
	defer endGroup()

	logger.Printf("Commits: %s\n", strings.Join(hashes, ", "))

	if headSha != "" && (!hashRegex.MatchString(headSha) || len(headSha) != 40) {
		return fmt.Errorf("invalid head-sha %q, must be a full 40 hex digit commit hash", headSha)
	}

	if createBranch && headSha == "" {
		return errors.New("cannot use --create-branch without supplying --head-sha")
	}

	token := getToken(os.Getenv)
	if token == "" {
		return errors.New("no GitHub token supplied")
	}

	client := NewClient(ctx, token, owner, repository, branch)
	client.dryrun = dryrun

	if headSha == "" {
		remoteSha, err := client.GetHeadCommitHash(context.Background())
		if err != nil {
			return err
		}
		headSha = remoteSha
	} else if createBranch {
		remoteSha, err := client.CreateBranch(ctx, headSha)
		if err != nil {
			return err
		}
		headSha = remoteSha
	}

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
