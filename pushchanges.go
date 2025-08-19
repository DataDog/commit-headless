package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

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

	log("Owner: %s\n", owner)
	log("Repository: %s\n", repository)
	log("Branch: %s\n", branch)
	log("Commits: %s\n", strings.Join(hashes, ", "))

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

	log("Remote head commit: %s\n", headSha)
	for _, c := range changes {
		log("Commit %s\n", c.hash)
		log("  Headline: %s\n", c.Headline())
		log("  Body: %s\n", c.Body())
		log("  Changed files: %d\n", len(c.entries))
		for p, content := range c.entries {
			action := "MODIFY"
			if len(content) == 0 {
				action = "DELETE"
			}
			log("    - %s: %s\n", action, p)
		}
	}

	pushed, newHead, err := client.PushChanges(ctx, headSha, changes...)
	if err != nil {
		return err
	} else if pushed != len(changes) {
		return fmt.Errorf("pushed %d of %d changes", pushed, len(changes))
	}

	log("Pushed %d commits.\n", len(changes))
	log("Branch URL: %s\n", client.browseCommitsURL())

	// The only thing that goes to standard output is the new head reference, allowing callers to
	// capture stdout if they need the reference.
	fmt.Println(newHead)

	return nil
}
