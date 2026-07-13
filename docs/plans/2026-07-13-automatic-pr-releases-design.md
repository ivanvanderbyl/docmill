# Automatic PR Releases Design

## Goal

Publish a new semantic version whenever a pull request is merged into `main`.
Each release must tag the merge commit and publish a GitHub Release with notes
generated from the changes since the previous release.

## Version selection

The merged pull request may carry one of these labels:

- `release:major`
- `release:minor`
- `release:patch`

When no release label is present, the workflow selects a patch release. If
conflicting release labels are present, the largest requested change wins:
major, then minor, then patch. The workflow calculates the next version from
the repository's latest stable `vMAJOR.MINOR.PATCH` tag.

## Workflow

A GitHub Actions workflow listens for closed pull requests targeting `main`
and proceeds only when the pull request was merged. Release jobs are serialised
so two closely merged pull requests cannot calculate and publish the same next
version.

The workflow checks out the merge commit with full tag history, determines the
version bump from the pull request labels, and calculates the next tag. It then
creates the tag on the pull request's merge commit and publishes a non-draft
GitHub Release. GitHub generates the release title and notes from the delta
between the previous tag and the new tag.

The workflow receives only the `contents: write` permission required to create
the tag and release. It does not use repository secrets or third-party release
services.

## Failure handling

Malformed or absent stable tags cause a clear failure rather than guessing a
version. The serialised job refreshes tags before calculating the next version.
GitHub API failures leave the workflow failed and visible; rerunning the job is
safe because it checks whether the calculated tag or release already exists.

## Validation

Version calculation lives in a small repository script with table-driven tests
covering default patch behaviour, each release label, conflicting labels,
invalid tags, and major/minor/patch rollover. The GitHub Actions workflow calls
that tested script, and its YAML structure is checked locally.
