# Project Guidelines

## Role and behavior

We are senior software engineers working on a product together.
The operator will give directions and context on the codebase.
We'll be working together so let's have a good time :)
What matters is good design, clean code and reducing maintenance.
See files under spc/ for project specification.
Software-driven development is used in this repo, check the skills catalog to familiarize yourself.

## Commits & Pull requests

Follow the basics of [conventional commits](https://www.conventionalcommits.org/en/v1.0.0/#summary), check git history for examples.
Examples: "refactor(cmd): remove unused jobs", "docs: update AGENTS.md"
Use conventional commits for PR titles and commit messages.
Scope commit/PR titles to the service when the change is service-specific, e.g. "fix(exchange): ...".

## Agent setup

Run `bin/setup` at session start if not already done — it is idempotent and safe to re-run.
It installs Go and Node via mise (.tool-versions), configures git auth for private modules using ssh,
and downloads dependencies for every service in the go.work workspace.

## Code Style

Keep comments short and sweet, don't document obvious code.
**Formatting:** We use `gofumpt`.

## Package Documentation

Every Go package contained by a directory should have a `doc.go` file with a package-level comment:

```go
// Package foobar <brief description of this package>
package foobar
```

When adding a new package, create a `doc.go` file following this pattern. `cmd/` packages (which are `package main`) are exempt.

## Build and Test Commands

Use a ./run script, inspect other repos for examples, e.g "./run lint"
If mockery is used, generate mocks before running tests and linting.
If lint, format and tests are passing, dev is complete.

## Architecture & Patterns

**Error Handling:** Don't panic. Return errors explicitly.
new env vars should be read in main.go (via internal/config)
**Domain driven design Layout:** Follow the patterns in the repo. Business logic goes into domain/

## Misc

avoid single letter vars if their scope is not small.
run formatter as last step after making code changes.
go: run go mod tidy after making changes to go.mod and dependencies (run it inside the specific service directory, then `go work sync` from the repo root).
go: write functions in call order — entry point first, then the functions it calls, and so on.
go: receivers and loop vars are exceptions to single-letter var names.
go: Wrap errors with context: "doing something: error 404".
go: Use `errors.Is` and `errors.As` for checking.
go: mock_*.go files can be ignored entirely while working, if there are test errors, regenerate mocks.
**Dependencies:** Use the standard library where possible, discuss to include 3rd party.
