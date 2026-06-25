# Finish a Release

Five stages, each gated separately. The shared branches require PRs (rulesets reject
direct pushes), so the merges happen as PRs that a human reviews and merges in the
GitHub UI. You create PRs and stop; you never merge them. Expect this flow to span
multiple turns, waiting on the user at each human step.

Maintainer operation: direct clone only.

## Stage 0: pre-flight and plan

After the universal pre-flight:

- `release/X.Y.Z` exists and local copy is up to date with origin.
- `main` is up to date with origin.
- The final tag `X.Y.Z` does not exist locally or on origin.
- Release notes for the version exist **on the release branch** (check the branch, not
  the local working tree):
  `git show origin/release/4.1.0:project-docs/release-notes/version-4.1.0.md` succeeds,
  and the documentation table of contents references them:
  `git show origin/release/4.1.0:project-docs/index.rst` contains
  `release-notes/version-4.1.0`. If either is missing, stop: the release is not ready
  to ship. The fix is a normal stabilization commit on the `release/` branch adding
  the notes file and the index entry (release documentation counts as stabilization
  work); it then reaches `develop` through the back-merge like any other fix.
- The version bump is committed on the release branch: the chart version there equals
  the target. Check `git show origin/release/4.1.0:installer/charts/educates-installer/Chart.yaml`
  reports `version: 4.1.0` (and `appVersion: 4.1.0`). If it still shows the previous
  release, stop: run the `make release-prep VERSION=4.1.0` stabilization step (see the
  stabilization-fix reference) and commit it on the release branch first.

Present the full five-stage plan with concrete values, then take the stages one at a
time.

## Stage 1: PR the release branch into main

1. [confirm] `gh pr create --base main --head release/4.1.0 --title "Release 4.1.0"`

Stop. The user (or another maintainer) reviews and merges this PR in the UI. Wait for
the user to say it is merged; verify with `git fetch origin` and confirm
`origin/main` now contains the release branch head.

## Stage 2: tag the final release on main

```
git switch main
git pull --ff-only
git tag 4.1.0
```

2. [confirm] `git push origin 4.1.0`

The pushed tag is immutable and triggers the CI release build. Show the tagged commit
(sha + subject) in the confirmation. `main` only ever carries final release tags.

## Stage 3: back-merge main into develop

So develop reflects exactly what shipped, including stabilization fixes and
version-bump commits:

```
git switch develop
git pull --ff-only
git switch -c merge/4.1.0-to-develop
git merge main
```

If the merge conflicts, stop and resolve with the user; the back-merge may not be
automatic. Then, gated:

3. [confirm] `git push -u origin merge/4.1.0-to-develop`
4. [confirm] `gh pr create --base develop --head merge/4.1.0-to-develop --title "Back-merge 4.1.0 into develop"`

Stop. Human reviews and merges in the UI.

## Stage 4: delete the finished release branch

Only after the back-merge PR is merged, so nothing is lost. The release branch is
ephemeral; the permanent record is the tag plus the merge commits.

5. [confirm] `git push origin --delete release/4.1.0`

Also delete the local `release/4.1.0` and `merge/4.1.0-to-develop` branches (low
risk, but mention it).
