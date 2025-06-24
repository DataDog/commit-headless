package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

// push actually performs the push
func push(target, branch string, commits []string) error {
	owner, reponame, _ := strings.Cut(target, "/")
	fmt.Printf("owner=%s, repo=%s\n", owner, reponame)

	token := os.Getenv("GH_TOKEN")
	if token == "" {
		return errors.New("no GH_TOKEN supplied")
	}

	client := NewClient(context.Background(), token, owner, reponame, branch)

	repo, err := Open("")
	if err != nil {
		return fmt.Errorf("git.open: %w", err)
	}

	changes, err := repo.Changes(commits...)
	if err != nil {
		return fmt.Errorf("repo.Changes: %w", err)
	}

	headoid, err := client.GetHeadCommitHash(context.Background())
	if err != nil {
		return err
	}

	fmt.Printf("head commit: %s\n", headoid)

	pushed, err := client.PushChanges(context.Background(), headoid, changes...)
	if err != nil {
		return err
	} else if pushed != len(changes) {
		return fmt.Errorf("only pushed %d of %d changes", pushed, len(changes))
	}

	return nil
}
