# Tag a Pre-release Build

Pre-release tags trigger the CI build and produce a GitHub release marked
pre-release. Placement is the rule this skill enforces, because GitHub cannot:

- `X.Y.Z-alpha.N` and `X.Y.Z-beta.N`: on `develop`, before feature freeze.
- `X.Y.Z-rc.N`: on the `release/X.Y.Z` branch, after feature freeze.

Maintainer operation: direct clone only.

## Ask first

- Which stage (alpha, beta, rc) and which version and number, if not stated. Do not
  guess the version.

## Per-operation checks

After the universal pre-flight:

- The tag string matches the version pattern exactly
  (`X.Y.Z-alpha.N` / `X.Y.Z-beta.N` / `X.Y.Z-rc.N`).
- The tag does not already exist locally (`git tag -l <tag>`) or on origin
  (`git ls-remote --tags origin <tag>`). Pushed version tags are immutable under the
  tag ruleset, so a collision cannot be fixed by re-pushing; if the build for an
  existing tag was bad, the answer is the next number.
- **Placement:** for rc, the target commit is on the matching `release/X.Y.Z` branch;
  for alpha/beta, the target commit is on `develop`. Check with
  `git branch -a --contains <commit>` (the current branch is the usual target).
  Refuse, with an explanation of where the tag belongs, if this fails. For an rc this
  also means the release branch must exist; if it does not, the release has not been
  cut and rc tagging is premature.
- The branch being tagged is up to date with the authority, so the tag lands on the
  commit the user thinks it will. Show the target commit (sha + subject line) as part
  of the confirmation.
- For **rc tags only**: check the release notes exist on the release branch
  (`project-docs/release-notes/version-X.Y.Z.md`, referenced from
  `project-docs/index.rst`). Missing notes do not block an rc, but warn: an rc is a
  concrete candidate to become the final release, and the finish-release checks
  refuse to ship without the notes, so an rc cut without them cannot be the build
  that actually ships. Alpha and beta tags get no such check; they are expected to
  predate the notes.

## Commands

```
git switch release/4.1.0        # or develop for alpha/beta
git tag 4.1.0-rc.1
```

Then, gated:

1. [confirm] `git push origin 4.1.0-rc.1`

Nothing pushes tags automatically; this explicit push is the trigger for the CI
release build.
