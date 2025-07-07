package main

import "fmt"

type VersionCmd struct{}

func (cmd *VersionCmd) Run() error {
	fmt.Printf("commit-headless version %s\n", VERSION)
	return nil
}
