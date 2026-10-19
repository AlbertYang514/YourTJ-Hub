# Release freeze verdict is the GitHub Actions job result

## Status

Accepted
Class: process

## Context and Problem Statement

[0099](0099-shared-dev-release-cycles.md) protects dev during a release cycle with the required status
check `release-freeze`. The ruleset requires that context from the GitHub Actions app. The controller
wrote it as a check run through the Checks API with the workflow token, while the workflow job itself
was named `gate`. GitHub did not count those API-created runs toward the ruleset: open PRs stayed
blocked with `"release-freeze" is expected` even with a successful run on the exact head, and
maintainers had to admin-merge after checking the result by hand.

## Decision Drivers

- The required check must come from a producer the ruleset actually accepts.
- Keep the freeze rules, admission semantics and trusted-main execution unchanged.
- State changes must still revoke or restore previously evaluated PR heads and merge groups.
- Fail closed; never fabricate a green check.

## Considered Options

- Name the Actions job `release-freeze`, make its exit code the verdict, and re-run it after state changes.
- Keep API-written check runs and remove the app restriction from the ruleset requirement.
- Keep API-written check runs, but write them with a dedicated GitHub App token.

## Decision Outcome

The freeze workflow's job is named `release-freeze` and runs trusted-main `cycle_control.py check-pr`,
which exits non-zero when the PR head or merge group may not merge. The controller no longer writes
check runs. Control's `project` job and dev pushes re-run the latest completed `release-freeze`
workflow run for each open dev PR head and queued merge-group SHA (`actions: write`); queued runs
evaluate the new state themselves and heads without a run are logged.

### Consequences

- Good: the ruleset sees a genuine Actions job check; no admin merge is needed for a green head.
- Good: freeze logic, admission binding and the trusted-main boundary are unchanged.
- Trade-off: a re-run uses the workflow file of the original run, so PRs opened before this change
  need a new event (push or reopen) to get the new job. Until main carries `check-pr`, the job fails closed.
- Trade-off: re-evaluation costs a short job per open PR instead of one API write.

## Pros and Cons of the Options

### Actions job is the check (chosen)
- Good: uses the producer the ruleset already trusts; no new credentials.
- Bad: re-runs are slower than direct writes and depend on an existing run.

### Drop the app restriction
- Good: no code change.
- Bad: any token with `checks: write` could then satisfy the freeze.

### Dedicated GitHub App
- Good: direct writes stay instant.
- Bad: a new long-lived credential and app to operate for one check.

## Links

- [0099](0099-shared-dev-release-cycles.md) — shared dev release cycles
- [Release cycles runbook](../operations/release-cycles.md)
