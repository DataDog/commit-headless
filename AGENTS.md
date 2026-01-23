# AGENTS.md

This file provides guidance for AI coding agents working with this repository.

## Project Overview

This is `commit-headless`, a CLI tool and GitHub Action for created signed remote commits from local
changes via the GitHub REST API.

The action implementation is in the `action/` branch and action releases are tagged with
`action/VERSION`. The contents of the action releases are prepared from the contents of the
`@./action-template` directory. See `.github/workflows/release.yml` for details on how this works.

## Permissions

You are allowed to:

- Read and modify any file in this repository
- Run any Go commands (`go build`, `go test`, `go mod tidy`, etc.)
- Run `git` commands for version control operations, but you should not make commits or push unless
  given explicit permissions
- Run `go mod` commands to update dependencies and tidy

## Building, Running, and Testing

You can build, run, or test using the `go` command. For instance `go build .` and `go test -v ./...`

## Guidelines

This project has the potential to perform destructive operations on a GitHub repository and care
should be taken.

When making changes, you should ensure the test suite passes. New code, where possible, should carry
accompanying tests.

Avoid adding dependencies unless adding the dependency provides a significant gain in readability,
useability, or security.

User-facing changes should come with updates to `@README.md` as well as the action README in
`@./action-template/README.md`. `@CHANGELOG.md` should be updated with a short summary of changes
taking special care to mention breaking changes.
