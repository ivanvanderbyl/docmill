# Automatic PR Releases Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Tag and publish a generated-notes GitHub Release after every pull request merge into `main`, using release labels to select the semantic-version bump and patch by default.

**Architecture:** A small tested Go package calculates the next stable semantic version from the latest tag and the merged pull request's labels. A GitHub Actions workflow serialises merged-PR release jobs, calls that calculator, pushes a tag on the exact merge commit, and creates a GitHub Release using GitHub's generated release notes.

**Tech Stack:** Go 1.26, GitHub Actions, GitHub CLI

---

### Task 1: Add semantic version calculation

**Files:**
- Create: `internal/releaseversion/releaseversion.go`
- Create: `internal/releaseversion/releaseversion_test.go`

**Step 1: Write the failing tests**

Add table-driven tests for patch-by-default, explicit patch/minor/major labels,
largest-bump-wins conflicts, component rollover, and invalid stable tags.

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/releaseversion`
Expected: FAIL because the package implementation does not exist.

**Step 3: Write the minimal implementation**

Implement `Next(current string, labels []string) (string, error)`. Accept only
stable tags shaped exactly like `vMAJOR.MINOR.PATCH`, determine the largest
requested bump, default to patch, reset lower components, and return the next
tag.

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/releaseversion`
Expected: PASS.

### Task 2: Add the workflow-facing command

**Files:**
- Create: `internal/releaseversion/cmd/main.go`
- Create: `internal/releaseversion/cmd/main_test.go`

**Step 1: Write the failing tests**

Test argument parsing and output for a current tag plus a comma-separated label
list, including the no-label default and invalid input.

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/releaseversion/cmd`
Expected: FAIL because the command implementation does not exist.

**Step 3: Write the minimal implementation**

Accept `-current` and `-labels`, call `releaseversion.Next`, print only the next
tag to stdout, and return a non-zero exit status with a useful stderr message
for invalid input.

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/releaseversion/cmd`
Expected: PASS.

### Task 3: Add merged-PR release automation

**Files:**
- Create: `.github/workflows/release.yml`

**Step 1: Add the workflow**

Trigger on `pull_request` `closed` events targeting `main`, guard the job with
`github.event.pull_request.merged == true`, grant `contents: write`, and
serialise jobs using a shared release concurrency group without cancelling an
in-progress release.

Check out `github.event.pull_request.merge_commit_sha` with full history, set up
the repository's Go version, refresh tags, find the latest stable tag, and call
the tested version calculator with the PR label names.

Create or verify the calculated tag on the exact merge commit, push it, and use
`gh release create --generate-notes --notes-start-tag` to publish the release.
Make reruns idempotent when the tag and release already exist, but fail if an
existing tag points to a different commit.

**Step 2: Validate workflow syntax**

Run: `ruby -e 'require "yaml"; YAML.load_file(".github/workflows/release.yml", aliases: true)'`
Expected: exit 0.

### Task 4: Verify the complete change

**Files:**
- Modify only if verification exposes an issue: files from Tasks 1-3

**Step 1: Format and run focused tests**

Run: `gofmt -w internal/releaseversion`
Run: `go test ./internal/releaseversion/...`
Expected: PASS.

**Step 2: Run the project validation suite**

Run: `task check`
Expected: PASS, with only documented environment-dependent skips.

**Step 3: Review the diff and working tree**

Run: `git diff --check`
Run: `git status --short`
Expected: no whitespace errors; only the planned implementation and plan file
are uncommitted.

**Step 4: Commit the implementation**

```bash
git add .github/workflows/release.yml internal/releaseversion docs/plans/2026-07-13-automatic-pr-releases.md
git commit -m "ci: release every merged pull request"
```
