package main

import "fmt"

type VersionCmd struct{}

func (cmd *VersionCmd) Run() error {
	fmt.Printf("commit-headless v%s\n", VERSION)
	return nil
}
